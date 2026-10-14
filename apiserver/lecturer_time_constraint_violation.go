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

func registerLecturerTimeConstraintViolationRoutes(s *Server, mux *http.ServeMux) {
	mux.HandleFunc("GET /lecturer-time-constraint-violations", s.getLecturerTimeConstraintViolations)
	mux.HandleFunc("GET /lecturer/{id}/time-constraint-violations", s.getLecturerTimeConstraintViolationsByLecturer)
	ignoreHandler := requireContentType("application/json", true)(http.HandlerFunc(s.ignoreLecturerConstraintViolation))
	mux.Handle("POST /lecturer-time-constraint-violations/ignore", ignoreHandler)
	unignoreHandler := requireContentType("application/json", true)(http.HandlerFunc(s.unignoreLecturerConstraintViolation))
	mux.Handle("DELETE /lecturer-time-constraint-violations/ignore", unignoreHandler)
	mux.HandleFunc("GET /lecturer-time-constraint-violations/ignored", s.getIgnoredLecturerConstraintViolations)
}

type LecturerTimeConstraintViolation struct {
	ConstraintID       int    `json:"constraint_id"`
	LecturerID         int    `json:"lecturer_id"`
	Soft               bool   `json:"soft"`
	Weekday            int    `json:"weekday"`
	ConstraintAt       string `json:"constraint_at"`
	ConstraintDuration string `json:"constraint_duration"`
	TimeSlotID         int    `json:"time_slot_id"`
	CourseID           int    `json:"course_id"`
	SlotAt             string `json:"slot_at"`
	SlotDuration       string `json:"slot_duration"`
}

//go:embed queries/get_lecturer_time_constraint_violations.sql
var getLecturerTimeConstraintViolationsQuery string

//go:embed queries/get_lecturer_time_constraint_violations_by_lecturer.sql
var getLecturerTimeConstraintViolationsByLecturerQuery string

func (s *Server) fetchLecturerTimeConstraintViolations(ctx context.Context) ([]LecturerTimeConstraintViolation, error) {
	rows, err := s.DB.Query(ctx, getLecturerTimeConstraintViolationsQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var violations []LecturerTimeConstraintViolation
	for rows.Next() {
		var v LecturerTimeConstraintViolation
		err := rows.Scan(
			&v.ConstraintID,
			&v.LecturerID,
			&v.Soft,
			&v.Weekday,
			&v.ConstraintAt,
			&v.ConstraintDuration,
			&v.TimeSlotID,
			&v.CourseID,
			&v.SlotAt,
			&v.SlotDuration,
		)
		if err != nil {
			return nil, err
		}
		violations = append(violations, v)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	if violations == nil {
		violations = []LecturerTimeConstraintViolation{}
	}

	return violations, nil
}

func (s *Server) fetchLecturerTimeConstraintViolationsByLecturer(ctx context.Context, lecturerID int) ([]LecturerTimeConstraintViolation, error) {
	rows, err := s.DB.Query(ctx, getLecturerTimeConstraintViolationsByLecturerQuery, lecturerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var violations []LecturerTimeConstraintViolation
	for rows.Next() {
		var v LecturerTimeConstraintViolation
		err := rows.Scan(
			&v.ConstraintID,
			&v.LecturerID,
			&v.Soft,
			&v.Weekday,
			&v.ConstraintAt,
			&v.ConstraintDuration,
			&v.TimeSlotID,
			&v.CourseID,
			&v.SlotAt,
			&v.SlotDuration,
		)
		if err != nil {
			return nil, err
		}
		violations = append(violations, v)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	if violations == nil {
		violations = []LecturerTimeConstraintViolation{}
	}

	return violations, nil
}

// Handler: GET /lecturer-time-constraint-violations; Get all lecturer time constraint violations
func (s *Server) getLecturerTimeConstraintViolations(w http.ResponseWriter, r *http.Request) {
	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	violations, err := s.fetchLecturerTimeConstraintViolations(ctx)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, violations)
}

// Handler: GET /lecturer/{id}/time-constraint-violations; Get all time constraint violations of a single lecturer
func (s *Server) getLecturerTimeConstraintViolationsByLecturer(w http.ResponseWriter, r *http.Request) {
	lecturerID, err := parseID(r.PathValue("id"))
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	violations, err := s.fetchLecturerTimeConstraintViolationsByLecturer(ctx, lecturerID)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, violations)
}

type IgnoreLecturerConstraintViolationRequest struct {
	ConstraintID *int `json:"constraint_id"`
	TimeSlotID   *int `json:"time_slot_id"`
}

type IgnoredLecturerConstraintViolation struct {
	ConstraintID int `json:"constraint_id"`
	TimeSlotID   int `json:"time_slot_id"`
}

//go:embed queries/ignore_lecturer_constraint_violation.sql
var ignoreLecturerConstraintViolationQuery string

//go:embed queries/unignore_lecturer_constraint_violation.sql
var unignoreLecturerConstraintViolationQuery string

//go:embed queries/get_ignored_lecturer_constraint_violations.sql
var getIgnoredLecturerConstraintViolationsQuery string

// Handler: POST /lecturer-time-constraint-violations/ignore; Ignore a lecturer time constraint violation
func (s *Server) ignoreLecturerConstraintViolation(w http.ResponseWriter, r *http.Request) {
	var request IgnoreLecturerConstraintViolationRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if request.ConstraintID == nil || request.TimeSlotID == nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	_, err := s.DB.Exec(ctx, ignoreLecturerConstraintViolationQuery, *request.ConstraintID, *request.TimeSlotID)
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

// Handler: DELETE /lecturer-time-constraint-violations/ignore; Unignore a lecturer time constraint violation
func (s *Server) unignoreLecturerConstraintViolation(w http.ResponseWriter, r *http.Request) {
	var request IgnoreLecturerConstraintViolationRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if request.ConstraintID == nil || request.TimeSlotID == nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	result, err := s.DB.Exec(ctx, unignoreLecturerConstraintViolationQuery, *request.ConstraintID, *request.TimeSlotID)
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

//go:embed queries/delete_solved_ignored_lecturer_time_constraint_violations.sql
var deleteSolvedIgnoredLecturerTimeConstraintViolationsQuery string

// Handler: GET /lecturer-time-constraint-violations/ignored; Get all ignored lecturer time constraint violations
func (s *Server) getIgnoredLecturerConstraintViolations(w http.ResponseWriter, r *http.Request) {
	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	// Delete irrelevant violation ignores (already solved)
	_, err := s.DB.Exec(ctx, deleteSolvedIgnoredLecturerTimeConstraintViolationsQuery)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	rows, err := s.DB.Query(ctx, getIgnoredLecturerConstraintViolationsQuery)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var ignored []IgnoredLecturerConstraintViolation
	for rows.Next() {
		var i IgnoredLecturerConstraintViolation
		err := rows.Scan(&i.ConstraintID, &i.TimeSlotID)
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
		ignored = []IgnoredLecturerConstraintViolation{}
	}

	writeJSON(w, http.StatusOK, ignored)
}
