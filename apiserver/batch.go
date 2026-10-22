package apiserver

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// POST /batch applies several writes in one transaction (used for undo/redo in the web client).
// Update/delete may carry "expect" values; if a row no longer matches, the batch fails with 409.

func registerBatchRoutes(s *Server, mux *http.ServeMux) {
	batchHandler := requireContentType("application/json", false)(http.HandlerFunc(s.batch))
	mux.Handle("POST /batch", batchHandler)
}

const maxBatchOps = 200

// BatchOp is a single operation of a POST /batch request.
type BatchOp struct {
	Kind     string                     `json:"kind"` // create, update, delete
	Resource string                     `json:"resource"`
	ID       *int                       `json:"id,omitempty"` // create: restore a deleted row under this id
	Data     map[string]json.RawMessage `json:"data,omitempty"`
	Set      map[string]json.RawMessage `json:"set,omitempty"`
	Expect   map[string]json.RawMessage `json:"expect,omitempty"`
}

// BatchRequest is the representation of the expected JSON body for the POST /batch endpoint.
type BatchRequest struct {
	Ops []BatchOp `json:"ops"`
}

// BatchResult contains the row before and after an operation (null for create/delete).
type BatchResult struct {
	Before any `json:"before"`
	After  any `json:"after"`
	// Rows removed as a side effect, e.g. ignored conflicts of a deleted slot
	Cascade []BatchCascade `json:"cascade,omitempty"`
}

// BatchCascade is a row of Resource removed as a side effect of an operation.
type BatchCascade struct {
	Resource string `json:"resource"`
	Row      any    `json:"row"`
}

// BatchResponse is returned by POST /batch on success.
type BatchResponse struct {
	Results []BatchResult `json:"results"`
}

// BatchErrorResponse is returned by POST /batch on failure; Op is the index of the failed op.
type BatchErrorResponse struct {
	Error string `json:"error"`
	Op    int    `json:"op"`
}

type batchError struct {
	status int
	msg    string
}

func (e *batchError) Error() string { return e.msg }

func badRequest(format string, a ...any) error {
	return &batchError{http.StatusBadRequest, fmt.Sprintf(format, a...)}
}

// batchFields reads typed values from an op's data/set/expect.
type batchFields struct {
	raw map[string]json.RawMessage
}

func (f batchFields) only(allowed ...string) error {
	for k := range f.raw {
		found := false
		for _, a := range allowed {
			found = found || k == a
		}
		if !found {
			return badRequest("unknown field %q", k)
		}
	}
	return nil
}

func (f batchFields) int(name string) (*int, error) {
	raw, ok := f.raw[name]
	if !ok || string(raw) == "null" {
		return nil, nil
	}
	var v int
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, badRequest("invalid value for %q", name)
	}
	return &v, nil
}

func (f batchFields) string(name string) (*string, error) {
	raw, ok := f.raw[name]
	if !ok || string(raw) == "null" {
		return nil, nil
	}
	var v string
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, badRequest("invalid value for %q", name)
	}
	return &v, nil
}

// batchRun holds the state of one batch transaction.
type batchRun struct {
	tx     pgx.Tx
	events []updateInfo
}

func (b *batchRun) fields(raw map[string]json.RawMessage) batchFields {
	return batchFields{raw: raw}
}

// rowID returns the id an update/delete op addresses.
func rowID(op BatchOp) (int, error) {
	if op.ID == nil {
		return 0, badRequest("%s %s needs an id", op.Kind, op.Resource)
	}
	return *op.ID, nil
}

//go:embed queries/batch/id_was_issued.sql
var idWasIssuedQuery string

// checkRestorableID rejects ids the table's sequence has not issued yet.
func (b *batchRun) checkRestorableID(ctx context.Context, table string, id int) error {
	var issued bool
	if err := b.tx.QueryRow(ctx, idWasIssuedQuery, table, id).Scan(&issued); err != nil {
		return err
	}
	if !issued {
		return badRequest("id %d was never issued for %s", id, table)
	}
	return nil
}

// Handler: POST /batch; Apply several create/update/delete operations in one transaction
func (s *Server) batch(w http.ResponseWriter, r *http.Request) {
	var request BatchRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if len(request.Ops) == 0 || len(request.Ops) > maxBatchOps {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	const timeout = 5 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	tx, err := s.DB.Begin(ctx)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit

	run := &batchRun{tx: tx}
	results := make([]BatchResult, 0, len(request.Ops))
	for i, op := range request.Ops {
		result, err := run.apply(ctx, op)
		if err != nil {
			writeBatchError(w, r, i, err)
			return
		}
		results = append(results, result)
	}

	if err := tx.Commit(ctx); err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Only broadcast after commit
	for _, e := range run.events {
		sendUpdate(e)
	}

	writeJSON(w, http.StatusOK, BatchResponse{Results: results})
}

func writeBatchError(w http.ResponseWriter, r *http.Request, op int, err error) {
	status := http.StatusInternalServerError
	msg := "Interner Fehler"

	var bErr *batchError
	var pgErr *pgconn.PgError
	switch {
	case errors.As(err, &bErr):
		status, msg = bErr.status, bErr.msg
	case errors.As(err, &pgErr) && pgErr.Code == "23505": // Unique constraint violation
		status, msg = http.StatusConflict, "Eintrag existiert bereits"
	case errors.As(err, &pgErr) && pgErr.Code == "23503": // Foreign key violation
		status, msg = http.StatusConflict, "Verknüpfter Eintrag existiert nicht (mehr) oder wird noch verwendet"
	case errors.As(err, &pgErr) && (pgErr.Code == "23514" || pgErr.Code[:2] == "22"): // Check constraint / invalid input
		status, msg = http.StatusBadRequest, "Ungültige Eingabe"
	default:
		logError(r, err)
	}
	logRequest(r, fmt.Sprintf("batch op %d failed: %s", op, err))
	writeJSON(w, status, BatchErrorResponse{Error: msg, Op: op})
}

func (b *batchRun) apply(ctx context.Context, op BatchOp) (BatchResult, error) {
	switch op.Resource {
	case "courseTimeSlot":
		switch op.Kind {
		case "create":
			return b.createCourseTimeSlot(ctx, op)
		case "update":
			return b.updateCourseTimeSlot(ctx, op)
		case "delete":
			return b.deleteCourseTimeSlot(ctx, op)
		}
	case "timeSlotRoomAssignment":
		switch op.Kind {
		case "create":
			return b.createTimeSlotRoomAssignment(ctx, op)
		case "delete":
			return b.deleteTimeSlotRoomAssignment(ctx, op)
		}
	case "courseColor":
		switch op.Kind {
		case "create":
			return b.createCourseColor(ctx, op)
		case "update":
			return b.updateCourseColor(ctx, op)
		case "delete":
			return b.deleteCourseColor(ctx, op)
		}
	case "courseColorAssignment":
		switch op.Kind {
		case "create":
			return b.createCourseColorAssignment(ctx, op)
		case "delete":
			return b.deleteCourseColorAssignment(ctx, op)
		}
	case "ignoredResourceConflict":
		switch op.Kind {
		case "create":
			return b.createIgnoredResourceConflict(ctx, op)
		case "delete":
			return b.deleteIgnoredResourceConflict(ctx, op)
		}
	case "ignoredLecturerConstraintViolation":
		switch op.Kind {
		case "create":
			return b.createIgnoredLecturerViolation(ctx, op)
		case "delete":
			return b.deleteIgnoredLecturerViolation(ctx, op)
		}
	}
	return BatchResult{}, badRequest("unsupported op %s %s", op.Kind, op.Resource)
}

// --- courseTimeSlot ---

var courseTimeSlotFields = []string{"course", "weekday", "at", "duration"}

type courseTimeSlotValues struct {
	course, weekday *int
	at, duration    *string
}

func readCourseTimeSlotValues(f batchFields, extra ...string) (v courseTimeSlotValues, err error) {
	if err = f.only(append(extra, courseTimeSlotFields...)...); err != nil {
		return v, err
	}
	if v.course, err = f.int("course"); err != nil {
		return v, err
	}
	if v.weekday, err = f.int("weekday"); err != nil {
		return v, err
	}
	if v.at, err = f.string("at"); err != nil {
		return v, err
	}
	v.duration, err = f.string("duration")
	return v, err
}

func (b *batchRun) createCourseTimeSlot(ctx context.Context, op BatchOp) (BatchResult, error) {
	f := b.fields(op.Data)
	v, err := readCourseTimeSlotValues(f, "one_off_date")
	if err != nil {
		return BatchResult{}, err
	}
	oneOffDate, err := f.string("one_off_date")
	if err != nil {
		return BatchResult{}, err
	}
	if v.course == nil || v.weekday == nil || v.at == nil || v.duration == nil {
		return BatchResult{}, badRequest("course, weekday, at and duration are required")
	}

	var row pgx.Row
	if op.ID != nil {
		if err := b.checkRestorableID(ctx, "course_time_slots", *op.ID); err != nil {
			return BatchResult{}, err
		}
		row = b.tx.QueryRow(ctx, restoreCourseTimeSlotQuery, *op.ID, v.course, v.weekday, v.at, v.duration, oneOffDate)
	} else {
		row = b.tx.QueryRow(ctx, createCourseTimeSlotQuery, v.course, v.weekday, v.at, v.duration, oneOffDate)
	}
	var t CourseTimeSlot
	if err := row.Scan(&t.ID, &t.Course, &t.Weekday, &t.At, &t.Duration, &t.OneOffDate); err != nil {
		return BatchResult{}, err
	}
	b.events = append(b.events, updateInfo{Resource: "courseTimeSlot", Operation: createOperation, Data: t})
	return BatchResult{After: t}, nil
}

//go:embed queries/batch/restore_course_time_slot.sql
var restoreCourseTimeSlotQuery string

//go:embed queries/batch/lock_course_time_slot.sql
var lockCourseTimeSlotQuery string

// lockCourseTimeSlot locks the row of op.ID and checks op.Expect against it.
func (b *batchRun) lockCourseTimeSlot(ctx context.Context, op BatchOp) (CourseTimeSlot, error) {
	var t CourseTimeSlot
	id, err := rowID(op)
	if err != nil {
		return t, err
	}
	expect, err := readCourseTimeSlotValues(b.fields(op.Expect))
	if err != nil {
		return t, err
	}

	var matches bool
	err = b.tx.QueryRow(ctx, lockCourseTimeSlotQuery, id, expect.course, expect.weekday, expect.at, expect.duration).
		Scan(&t.ID, &t.Course, &t.Weekday, &t.At, &t.Duration, &t.OneOffDate, &matches)
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) {
		return t, &batchError{http.StatusNotFound, "Der Termin existiert nicht mehr"}
	}
	if err != nil {
		return t, err
	}
	if !matches {
		return t, &batchError{http.StatusConflict, "Der Termin wurde inzwischen von jemand anderem geändert"}
	}
	return t, nil
}

func (b *batchRun) updateCourseTimeSlot(ctx context.Context, op BatchOp) (BatchResult, error) {
	set, err := readCourseTimeSlotValues(b.fields(op.Set))
	if err != nil {
		return BatchResult{}, err
	}
	before, err := b.lockCourseTimeSlot(ctx, op)
	if err != nil {
		return BatchResult{}, err
	}

	var after CourseTimeSlot
	err = b.tx.QueryRow(ctx, updateCourseTimeSlotQuery, set.course, set.weekday, set.at, set.duration, nil, before.ID).
		Scan(&after.ID, &after.Course, &after.Weekday, &after.At, &after.Duration, &after.OneOffDate)
	if err != nil {
		return BatchResult{}, err
	}
	b.events = append(b.events, updateInfo{Resource: "courseTimeSlot", Operation: updateOperation, Data: after})
	return BatchResult{Before: before, After: after}, nil
}

func (b *batchRun) deleteCourseTimeSlot(ctx context.Context, op BatchOp) (BatchResult, error) {
	before, err := b.lockCourseTimeSlot(ctx, op)
	if err != nil {
		return BatchResult{}, err
	}
	// Delete ignored conflicts explicitly (not via FK cascade) so undo can restore them
	cascade, err := b.deleteIgnoresOfSlot(ctx, before.ID)
	if err != nil {
		return BatchResult{}, err
	}
	if _, err := b.tx.Exec(ctx, deleteCourseTimeSlotQuery, before.ID); err != nil {
		return BatchResult{}, err
	}
	b.events = append(b.events, updateInfo{Resource: "courseTimeSlot", Operation: deletionOperation, Data: before.ID})
	return BatchResult{Before: before, Cascade: cascade}, nil
}

// --- timeSlotRoomAssignment ---

func (b *batchRun) timeSlotRoomAssignment(op BatchOp) (TimeSlotRoomAssignment, error) {
	f := b.fields(op.Data)
	if err := f.only("time_slot", "room"); err != nil {
		return TimeSlotRoomAssignment{}, err
	}
	timeSlot, err := f.int("time_slot")
	if err != nil {
		return TimeSlotRoomAssignment{}, err
	}
	room, err := f.int("room")
	if err != nil {
		return TimeSlotRoomAssignment{}, err
	}
	if timeSlot == nil || room == nil {
		return TimeSlotRoomAssignment{}, badRequest("time_slot and room are required")
	}
	return TimeSlotRoomAssignment{TimeSlot: *timeSlot, Room: *room}, nil
}

func (b *batchRun) createTimeSlotRoomAssignment(ctx context.Context, op BatchOp) (BatchResult, error) {
	a, err := b.timeSlotRoomAssignment(op)
	if err != nil {
		return BatchResult{}, err
	}
	if _, err := b.tx.Exec(ctx, createTimeSlotRoomAssignmentQuery, a.TimeSlot, a.Room); err != nil {
		return BatchResult{}, err
	}
	b.events = append(b.events, updateInfo{Resource: "timeSlotRoomAssignment", Operation: createOperation, Data: a})
	return BatchResult{After: a}, nil
}

func (b *batchRun) deleteTimeSlotRoomAssignment(ctx context.Context, op BatchOp) (BatchResult, error) {
	a, err := b.timeSlotRoomAssignment(op)
	if err != nil {
		return BatchResult{}, err
	}
	result, err := b.tx.Exec(ctx, deleteTimeSlotRoomAssignmentQuery, a.TimeSlot, a.Room)
	if err != nil {
		return BatchResult{}, err
	}
	if result.RowsAffected() == 0 {
		return BatchResult{}, &batchError{http.StatusNotFound, "Die Raumzuweisung existiert nicht mehr"}
	}
	b.events = append(b.events, updateInfo{Resource: "timeSlotRoomAssignment", Operation: deletionOperation, Data: a})
	return BatchResult{Before: a}, nil
}

// --- courseColor ---

type courseColorValues struct {
	name, hex *string
}

func readCourseColorValues(f batchFields) (v courseColorValues, err error) {
	if err = f.only("name", "hex"); err != nil {
		return v, err
	}
	if v.name, err = f.string("name"); err != nil {
		return v, err
	}
	if v.hex, err = f.string("hex"); err != nil {
		return v, err
	}
	if v.hex != nil && !hexPattern.MatchString(*v.hex) {
		return v, badRequest("invalid hex %q", *v.hex)
	}
	return v, nil
}

func (b *batchRun) createCourseColor(ctx context.Context, op BatchOp) (BatchResult, error) {
	v, err := readCourseColorValues(b.fields(op.Data))
	if err != nil {
		return BatchResult{}, err
	}
	if v.name == nil || v.hex == nil {
		return BatchResult{}, badRequest("name and hex are required")
	}

	var row pgx.Row
	if op.ID != nil {
		if err := b.checkRestorableID(ctx, "course_color", *op.ID); err != nil {
			return BatchResult{}, err
		}
		row = b.tx.QueryRow(ctx, restoreCourseColorQuery, *op.ID, v.name, v.hex)
	} else {
		row = b.tx.QueryRow(ctx, createCourseColorQuery, v.name, v.hex)
	}
	var c CourseColor
	if err := row.Scan(&c.ID, &c.Name, &c.Hex); err != nil {
		return BatchResult{}, err
	}
	b.events = append(b.events, updateInfo{Resource: "courseColor", Operation: createOperation, Data: c})
	return BatchResult{After: c}, nil
}

//go:embed queries/batch/lock_course_color.sql
var lockCourseColorQuery string

//go:embed queries/batch/restore_course_color.sql
var restoreCourseColorQuery string

// lockCourseColor locks the row of op.ID and checks op.Expect against it.
func (b *batchRun) lockCourseColor(ctx context.Context, op BatchOp) (CourseColor, error) {
	var c CourseColor
	id, err := rowID(op)
	if err != nil {
		return c, err
	}
	expect, err := readCourseColorValues(b.fields(op.Expect))
	if err != nil {
		return c, err
	}

	var matches bool
	err = b.tx.QueryRow(ctx, lockCourseColorQuery, id, expect.name, expect.hex).Scan(&c.ID, &c.Name, &c.Hex, &matches)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, &batchError{http.StatusNotFound, "Die Farbe existiert nicht mehr"}
	}
	if err != nil {
		return c, err
	}
	if !matches {
		return c, &batchError{http.StatusConflict, "Die Farbe wurde inzwischen von jemand anderem geändert"}
	}
	return c, nil
}

func (b *batchRun) updateCourseColor(ctx context.Context, op BatchOp) (BatchResult, error) {
	set, err := readCourseColorValues(b.fields(op.Set))
	if err != nil {
		return BatchResult{}, err
	}
	before, err := b.lockCourseColor(ctx, op)
	if err != nil {
		return BatchResult{}, err
	}

	var after CourseColor
	err = b.tx.QueryRow(ctx, updateCourseColorQuery, set.name, set.hex, before.ID).Scan(&after.ID, &after.Name, &after.Hex)
	if err != nil {
		return BatchResult{}, err
	}
	b.events = append(b.events, updateInfo{Resource: "courseColor", Operation: updateOperation, Data: after})
	return BatchResult{Before: before, After: after}, nil
}

func (b *batchRun) deleteCourseColor(ctx context.Context, op BatchOp) (BatchResult, error) {
	before, err := b.lockCourseColor(ctx, op)
	if err != nil {
		return BatchResult{}, err
	}
	if _, err := b.tx.Exec(ctx, deleteCourseColorQuery, before.ID); err != nil {
		return BatchResult{}, err
	}
	b.events = append(b.events, updateInfo{Resource: "courseColor", Operation: deletionOperation, Data: before.ID})
	return BatchResult{Before: before}, nil
}

// --- courseColorAssignment ---

func (b *batchRun) courseColorAssignment(op BatchOp) (CourseColorAssignment, error) {
	f := b.fields(op.Data)
	if err := f.only("course", "color"); err != nil {
		return CourseColorAssignment{}, err
	}
	course, err := f.int("course")
	if err != nil {
		return CourseColorAssignment{}, err
	}
	color, err := f.int("color")
	if err != nil {
		return CourseColorAssignment{}, err
	}
	if course == nil || color == nil {
		return CourseColorAssignment{}, badRequest("course and color are required")
	}
	return CourseColorAssignment{Course: *course, Color: *color}, nil
}

func (b *batchRun) createCourseColorAssignment(ctx context.Context, op BatchOp) (BatchResult, error) {
	a, err := b.courseColorAssignment(op)
	if err != nil {
		return BatchResult{}, err
	}
	if _, err := b.tx.Exec(ctx, createCourseColorAssignmentQuery, a.Course, a.Color); err != nil {
		return BatchResult{}, err
	}
	b.events = append(b.events, updateInfo{Resource: "courseColorAssignment", Operation: createOperation, Data: a})
	return BatchResult{After: a}, nil
}

func (b *batchRun) deleteCourseColorAssignment(ctx context.Context, op BatchOp) (BatchResult, error) {
	a, err := b.courseColorAssignment(op)
	if err != nil {
		return BatchResult{}, err
	}
	result, err := b.tx.Exec(ctx, deleteCourseColorAssignmentQuery, a.Course, a.Color)
	if err != nil {
		return BatchResult{}, err
	}
	if result.RowsAffected() == 0 {
		return BatchResult{}, &batchError{http.StatusConflict, "Die Farbe der Veranstaltung wurde inzwischen von jemand anderem geändert"}
	}
	b.events = append(b.events, updateInfo{Resource: "courseColorAssignment", Operation: deletionOperation, Data: a})
	return BatchResult{Before: a}, nil
}

// --- ignored conflicts (lenient: rows that no longer exist are skipped) ---

//go:embed queries/batch/delete_ignores_of_slot.sql
var deleteIgnoresOfSlotQuery string

//go:embed queries/batch/delete_lecturer_ignores_of_slot.sql
var deleteLecturerIgnoresOfSlotQuery string

// deleteIgnoresOfSlot removes all ignored conflicts of a slot and returns them as cascade rows.
func (b *batchRun) deleteIgnoresOfSlot(ctx context.Context, slotID int) ([]BatchCascade, error) {
	var cascade []BatchCascade

	rows, err := b.tx.Query(ctx, deleteIgnoresOfSlotQuery, slotID)
	if err != nil {
		return nil, err
	}
	ignored, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (IgnoredResourceConflict, error) {
		var i IgnoredResourceConflict
		err := row.Scan(&i.SlotA, &i.SlotB, &i.ResourceType, &i.ResourceID)
		return i, err
	})
	if err != nil {
		return nil, err
	}
	for _, i := range ignored {
		cascade = append(cascade, BatchCascade{Resource: "ignoredResourceConflict", Row: i})
	}

	rows, err = b.tx.Query(ctx, deleteLecturerIgnoresOfSlotQuery, slotID)
	if err != nil {
		return nil, err
	}
	lecturerIgnored, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (IgnoredLecturerConstraintViolation, error) {
		var i IgnoredLecturerConstraintViolation
		err := row.Scan(&i.ConstraintID, &i.TimeSlotID)
		return i, err
	})
	if err != nil {
		return nil, err
	}
	for _, i := range lecturerIgnored {
		cascade = append(cascade, BatchCascade{Resource: "ignoredLecturerConstraintViolation", Row: i})
	}
	return cascade, nil
}

func (b *batchRun) ignoredResourceConflict(op BatchOp) (IgnoredResourceConflict, error) {
	f := b.fields(op.Data)
	if err := f.only("slot_a", "slot_b", "resource_type", "resource_id"); err != nil {
		return IgnoredResourceConflict{}, err
	}
	slotA, err := f.int("slot_a")
	if err != nil {
		return IgnoredResourceConflict{}, err
	}
	slotB, err := f.int("slot_b")
	if err != nil {
		return IgnoredResourceConflict{}, err
	}
	resourceType, err := f.string("resource_type")
	if err != nil {
		return IgnoredResourceConflict{}, err
	}
	resourceID, err := f.int("resource_id")
	if err != nil {
		return IgnoredResourceConflict{}, err
	}
	if slotA == nil || slotB == nil || resourceType == nil || resourceID == nil {
		return IgnoredResourceConflict{}, badRequest("slot_a, slot_b, resource_type and resource_id are required")
	}
	return IgnoredResourceConflict{SlotA: *slotA, SlotB: *slotB, ResourceType: *resourceType, ResourceID: *resourceID}, nil
}

//go:embed queries/batch/restore_ignored_resource_conflict.sql
var restoreIgnoredResourceConflictQuery string

//go:embed queries/batch/delete_ignored_resource_conflict.sql
var deleteIgnoredResourceConflictQuery string

// ignoreRowOrNil returns nil if the lenient query affected no row.
func ignoreRowOrNil[T any](row pgx.Row, scan func(pgx.Row, *T) error) (*T, error) {
	var v T
	err := scan(row, &v)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func scanIgnoredResourceConflict(row pgx.Row, i *IgnoredResourceConflict) error {
	return row.Scan(&i.SlotA, &i.SlotB, &i.ResourceType, &i.ResourceID)
}

func scanIgnoredLecturerViolation(row pgx.Row, i *IgnoredLecturerConstraintViolation) error {
	return row.Scan(&i.ConstraintID, &i.TimeSlotID)
}

func (b *batchRun) createIgnoredResourceConflict(ctx context.Context, op BatchOp) (BatchResult, error) {
	i, err := b.ignoredResourceConflict(op)
	if err != nil {
		return BatchResult{}, err
	}
	after, err := ignoreRowOrNil(b.tx.QueryRow(ctx, restoreIgnoredResourceConflictQuery, i.SlotA, i.SlotB, i.ResourceType, i.ResourceID), scanIgnoredResourceConflict)
	if err != nil || after == nil {
		return BatchResult{}, err
	}
	return BatchResult{After: after}, nil
}

func (b *batchRun) deleteIgnoredResourceConflict(ctx context.Context, op BatchOp) (BatchResult, error) {
	i, err := b.ignoredResourceConflict(op)
	if err != nil {
		return BatchResult{}, err
	}
	before, err := ignoreRowOrNil(b.tx.QueryRow(ctx, deleteIgnoredResourceConflictQuery, i.SlotA, i.SlotB, i.ResourceType, i.ResourceID), scanIgnoredResourceConflict)
	if err != nil || before == nil {
		return BatchResult{}, err
	}
	return BatchResult{Before: before}, nil
}

func (b *batchRun) ignoredLecturerViolation(op BatchOp) (IgnoredLecturerConstraintViolation, error) {
	f := b.fields(op.Data)
	if err := f.only("constraint_id", "time_slot_id"); err != nil {
		return IgnoredLecturerConstraintViolation{}, err
	}
	constraintID, err := f.int("constraint_id")
	if err != nil {
		return IgnoredLecturerConstraintViolation{}, err
	}
	timeSlotID, err := f.int("time_slot_id")
	if err != nil {
		return IgnoredLecturerConstraintViolation{}, err
	}
	if constraintID == nil || timeSlotID == nil {
		return IgnoredLecturerConstraintViolation{}, badRequest("constraint_id and time_slot_id are required")
	}
	return IgnoredLecturerConstraintViolation{ConstraintID: *constraintID, TimeSlotID: *timeSlotID}, nil
}

//go:embed queries/batch/restore_ignored_lecturer_violation.sql
var restoreIgnoredLecturerViolationQuery string

//go:embed queries/batch/delete_ignored_lecturer_violation.sql
var deleteIgnoredLecturerViolationQuery string

func (b *batchRun) createIgnoredLecturerViolation(ctx context.Context, op BatchOp) (BatchResult, error) {
	i, err := b.ignoredLecturerViolation(op)
	if err != nil {
		return BatchResult{}, err
	}
	after, err := ignoreRowOrNil(b.tx.QueryRow(ctx, restoreIgnoredLecturerViolationQuery, i.ConstraintID, i.TimeSlotID), scanIgnoredLecturerViolation)
	if err != nil || after == nil {
		return BatchResult{}, err
	}
	return BatchResult{After: after}, nil
}

func (b *batchRun) deleteIgnoredLecturerViolation(ctx context.Context, op BatchOp) (BatchResult, error) {
	i, err := b.ignoredLecturerViolation(op)
	if err != nil {
		return BatchResult{}, err
	}
	before, err := ignoreRowOrNil(b.tx.QueryRow(ctx, deleteIgnoredLecturerViolationQuery, i.ConstraintID, i.TimeSlotID), scanIgnoredLecturerViolation)
	if err != nil || before == nil {
		return BatchResult{}, err
	}
	return BatchResult{Before: before}, nil
}
