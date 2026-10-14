package apiserver

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func registerResourceConflictRoutes(s *Server, mux *http.ServeMux) {
	mux.HandleFunc("GET /resource-constraint-violations", s.getResourceConflicts)
	mux.HandleFunc("GET /room/{id}/constraint-violations", s.getResourceConflictsByResource("room"))
	mux.HandleFunc("GET /lecturer/{id}/constraint-violations", s.getResourceConflictsByResource("lecturer"))
	mux.HandleFunc("GET /study-group/{id}/constraint-violations", s.getResourceConflictsByResource("study_group"))
	mux.HandleFunc("GET /study-group/{id}/related-constraint-violations", s.getStudyGroupRelatedConstraintViolations)
	ignoreHandler := requireContentType("application/json", true)(http.HandlerFunc(s.ignoreResourceConflict))
	mux.Handle("POST /resource-constraint-violations/ignore", ignoreHandler)
	unignoreHandler := requireContentType("application/json", true)(http.HandlerFunc(s.unignoreResourceConflict))
	mux.Handle("DELETE /resource-constraint-violations/ignore", unignoreHandler)
	mux.HandleFunc("GET /resource-constraint-violations/ignored", s.getIgnoredResourceConflicts)
}

type ResourceConflict struct {
	ResourceType string `json:"resource_type"`
	ResourceID   int    `json:"resource_id"`
	Weekday      int    `json:"weekday"`
	SlotAID      int    `json:"slot_a_id"`
	SlotACourse  int    `json:"slot_a_course"`
	SlotAAt      string `json:"slot_a_at"`
	SlotADur     string `json:"slot_a_duration"`
	SlotBID      int    `json:"slot_b_id"`
	SlotBCourse  int    `json:"slot_b_course"`
	SlotBAt      string `json:"slot_b_at"`
	SlotBDur     string `json:"slot_b_duration"`
}

//go:embed queries/get_resource_conflicts.sql
var getResourceConflictsQuery string

//go:embed queries/get_resource_conflicts_by_resource.sql
var getResourceConflictsByResourceQuery string

func (s *Server) fetchResourceConflicts(ctx context.Context) ([]ResourceConflict, error) {
	rows, err := s.DB.Query(ctx, getResourceConflictsQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var conflicts []ResourceConflict
	for rows.Next() {
		var c ResourceConflict
		err := rows.Scan(
			&c.ResourceType,
			&c.ResourceID,
			&c.Weekday,
			&c.SlotAID,
			&c.SlotACourse,
			&c.SlotAAt,
			&c.SlotADur,
			&c.SlotBID,
			&c.SlotBCourse,
			&c.SlotBAt,
			&c.SlotBDur,
		)
		if err != nil {
			return nil, err
		}
		conflicts = append(conflicts, c)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	if conflicts == nil {
		conflicts = []ResourceConflict{}
	}

	return conflicts, nil
}

func (s *Server) fetchResourceConflictsByResource(ctx context.Context, resourceType string, resourceID int) ([]ResourceConflict, error) {
	rows, err := s.DB.Query(ctx, getResourceConflictsByResourceQuery, resourceType, resourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var conflicts []ResourceConflict
	for rows.Next() {
		var c ResourceConflict
		err := rows.Scan(
			&c.ResourceType,
			&c.ResourceID,
			&c.Weekday,
			&c.SlotAID,
			&c.SlotACourse,
			&c.SlotAAt,
			&c.SlotADur,
			&c.SlotBID,
			&c.SlotBCourse,
			&c.SlotBAt,
			&c.SlotBDur,
		)
		if err != nil {
			return nil, err
		}
		conflicts = append(conflicts, c)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	if conflicts == nil {
		conflicts = []ResourceConflict{}
	}

	return conflicts, nil
}

// Handler: GET /room/{id}/constraint-violations,
// GET /lecturer/{id}/constraint-violations,
// GET /study-group/{id}/constraint-violations;
// Get all resource constraint violations of a single resource
func (s *Server) getResourceConflictsByResource(resourceType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resourceID, err := parseID(r.PathValue("id"))
		if err != nil {
			logError(r, err)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		const timeout = 3 * time.Second
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()

		conflicts, err := s.fetchResourceConflictsByResource(ctx, resourceType, resourceID)
		if err != nil {
			logError(r, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, conflicts)
	}
}

// RelatedConstraintViolations combines resource and time constraint violations for storing all conflicts associated with a study group
type RelatedConstraintViolations struct {
	ResourceConflicts                []ResourceConflict                `json:"resource_conflicts"`
	LecturerTimeConstraintViolations []LecturerTimeConstraintViolation `json:"lecturer_time_constraint_violations"`
}

//go:embed queries/get_related_resource_conflicts_by_study_group.sql
var getRelatedResourceConflictsByStudyGroupQuery string

//go:embed queries/get_related_lecturer_violations_by_study_group.sql
var getRelatedLecturerViolationsByStudyGroupQuery string

// Handler: GET /study-group/{id}/related-constraint-violations; Get all conflicts and violations related to a study group
func (s *Server) getStudyGroupRelatedConstraintViolations(w http.ResponseWriter, r *http.Request) {
	studyGroupID, err := parseID(r.PathValue("id"))
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	// Fetch resource conflicts
	conflictRows, err := s.DB.Query(ctx, getRelatedResourceConflictsByStudyGroupQuery, studyGroupID)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer conflictRows.Close()

	var conflicts []ResourceConflict
	for conflictRows.Next() {
		var c ResourceConflict
		err := conflictRows.Scan(
			&c.ResourceType, &c.ResourceID, &c.Weekday,
			&c.SlotAID, &c.SlotACourse, &c.SlotAAt, &c.SlotADur,
			&c.SlotBID, &c.SlotBCourse, &c.SlotBAt, &c.SlotBDur,
		)
		if err != nil {
			logError(r, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		conflicts = append(conflicts, c)
	}
	if err = conflictRows.Err(); err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if conflicts == nil {
		conflicts = []ResourceConflict{}
	}

	// Fetch lecturer time constraint violations
	violationRows, err := s.DB.Query(ctx, getRelatedLecturerViolationsByStudyGroupQuery, studyGroupID)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer violationRows.Close()

	var violations []LecturerTimeConstraintViolation
	for violationRows.Next() {
		var v LecturerTimeConstraintViolation
		err := violationRows.Scan(
			&v.ConstraintID, &v.LecturerID, &v.Soft, &v.Weekday,
			&v.ConstraintAt, &v.ConstraintDuration,
			&v.TimeSlotID, &v.CourseID, &v.SlotAt, &v.SlotDuration,
		)
		if err != nil {
			logError(r, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		violations = append(violations, v)
	}
	if err = violationRows.Err(); err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if violations == nil {
		violations = []LecturerTimeConstraintViolation{}
	}

	writeJSON(w, http.StatusOK, RelatedConstraintViolations{
		ResourceConflicts:                conflicts,
		LecturerTimeConstraintViolations: violations,
	})
}

// Handler: GET /resource-constraint-violations; Get all resource constraint violations
func (s *Server) getResourceConflicts(w http.ResponseWriter, r *http.Request) {
	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	conflicts, err := s.fetchResourceConflicts(ctx)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, conflicts)
}

type IgnoreResourceConflictRequest struct {
	SlotAID      *int    `json:"slot_a_id"`
	SlotBID      *int    `json:"slot_b_id"`
	ResourceType *string `json:"resource_type"`
	ResourceID   *int    `json:"resource_id"`
}

type IgnoredResourceConflict struct {
	SlotA        int    `json:"slot_a"`
	SlotB        int    `json:"slot_b"`
	ResourceType string `json:"resource_type"`
	ResourceID   int    `json:"resource_id"`
}

//go:embed queries/ignore_resource_conflict.sql
var ignoreResourceConflictQuery string

//go:embed queries/unignore_resource_conflict.sql
var unignoreResourceConflictQuery string

//go:embed queries/get_ignored_resource_conflicts.sql
var getIgnoredResourceConflictsQuery string

// Handler: POST /resource-constraint-violations/ignore; Ignore a resource constraint violation
func (s *Server) ignoreResourceConflict(w http.ResponseWriter, r *http.Request) {
	var request IgnoreResourceConflictRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if request.SlotAID == nil || request.SlotBID == nil || request.ResourceType == nil || request.ResourceID == nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	_, err := s.DB.Exec(ctx, ignoreResourceConflictQuery, *request.SlotAID, *request.SlotBID, *request.ResourceType, *request.ResourceID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // Unique constraint violation / already ignored
			w.WriteHeader(http.StatusConflict)
		} else {
			logError(r, err)
			w.WriteHeader(http.StatusInternalServerError)
		}
		return
	}

	w.WriteHeader(http.StatusCreated)
}

// Handler: DELETE /resource-constraint-violations/ignore; Unignore a resource constraint violation
func (s *Server) unignoreResourceConflict(w http.ResponseWriter, r *http.Request) {
	var request IgnoreResourceConflictRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if request.SlotAID == nil || request.SlotBID == nil || request.ResourceType == nil || request.ResourceID == nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	result, err := s.DB.Exec(ctx, unignoreResourceConflictQuery, *request.SlotAID, *request.SlotBID, *request.ResourceType, *request.ResourceID)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if result.RowsAffected() == 0 {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusOK)
}

//go:embed queries/delete_solved_ignored_resource_conflicts.sql
var deleteSolvedIgnoredResourceConflictsQuery string

// Handler: GET /resource-constraint-violations/ignored; Get all ignored resource constraint violations
func (s *Server) getIgnoredResourceConflicts(w http.ResponseWriter, r *http.Request) {
	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	// Delete irrelevant violation ignores
	_, err := s.DB.Exec(ctx, deleteSolvedIgnoredResourceConflictsQuery)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	rows, err := s.DB.Query(ctx, getIgnoredResourceConflictsQuery)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var ignored []IgnoredResourceConflict
	for rows.Next() {
		var i IgnoredResourceConflict
		err := rows.Scan(&i.SlotA, &i.SlotB, &i.ResourceType, &i.ResourceID)
		if err != nil {
			logError(r, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		ignored = append(ignored, i)
	}
	if err = rows.Err(); err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if ignored == nil {
		ignored = []IgnoredResourceConflict{}
	}

	writeJSON(w, http.StatusOK, ignored)
}
