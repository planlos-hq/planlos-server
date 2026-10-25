package apiserver

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func registerCourseLecturerViolationRoutes(s *Server, mux *http.ServeMux) {
	mux.HandleFunc("GET /course-lecturer-violations", s.getCourseLecturerViolations)
	ignoreHandler := requireContentType("application/json", true)(http.HandlerFunc(s.ignoreCourseLecturerViolation))
	mux.Handle("POST /course-lecturer-violations/ignore", ignoreHandler)
	unignoreHandler := requireContentType("application/json", true)(http.HandlerFunc(s.unignoreCourseLecturerViolation))
	mux.Handle("DELETE /course-lecturer-violations/ignore", unignoreHandler)
	mux.HandleFunc("GET /course-lecturer-violations/ignored", s.getIgnoredCourseLecturerViolations)
}

// CourseLecturerViolation is a course that is taught by lab engineers only.
type CourseLecturerViolation struct {
	CourseID         int    `json:"course_id"`
	Number           string `json:"number"`
	Name             string `json:"name"`
	LabEngineerCount int    `json:"lab_engineer_count"`
}

// IgnoreCourseLecturerViolationRequest is the expected JSON body for (un)ignoring a course lecturer violation.
type IgnoreCourseLecturerViolationRequest struct {
	CourseID *int `json:"course_id"`
}

// IgnoredCourseLecturerViolation is a course whose lecturer violation is ignored.
type IgnoredCourseLecturerViolation struct {
	CourseID int `json:"course_id"`
}

//go:embed queries/get_course_lecturer_violations.sql
var getCourseLecturerViolationsQuery string

//go:embed queries/ignore_course_lecturer_violation.sql
var ignoreCourseLecturerViolationQuery string

//go:embed queries/unignore_course_lecturer_violation.sql
var unignoreCourseLecturerViolationQuery string

//go:embed queries/get_ignored_course_lecturer_violations.sql
var getIgnoredCourseLecturerViolationsQuery string

//go:embed queries/delete_solved_ignored_course_lecturer_violations.sql
var deleteSolvedIgnoredCourseLecturerViolationsQuery string

// Handler: GET /course-lecturer-violations; Get all courses that only have lab engineers assigned
func (s *Server) getCourseLecturerViolations(w http.ResponseWriter, r *http.Request) {
	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	rows, err := s.DB.Query(ctx, getCourseLecturerViolationsQuery)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	violations, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (CourseLecturerViolation, error) {
		var v CourseLecturerViolation
		err := row.Scan(&v.CourseID, &v.Number, &v.Name, &v.LabEngineerCount)
		return v, err
	})
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if violations == nil {
		violations = []CourseLecturerViolation{}
	}

	writeJSON(w, http.StatusOK, violations)
}

// readIgnoreCourseLecturerViolationRequest decodes the body; ok is false if a 400 was written.
func readIgnoreCourseLecturerViolationRequest(w http.ResponseWriter, r *http.Request) (int, bool) {
	var request IgnoreCourseLecturerViolationRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusBadRequest)
		return 0, false
	}
	if request.CourseID == nil {
		w.WriteHeader(http.StatusBadRequest)
		return 0, false
	}
	return *request.CourseID, true
}

// Handler: POST /course-lecturer-violations/ignore; Ignore the lecturer violation of a course
func (s *Server) ignoreCourseLecturerViolation(w http.ResponseWriter, r *http.Request) {
	courseID, ok := readIgnoreCourseLecturerViolationRequest(w, r)
	if !ok {
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	_, err := s.DB.Exec(ctx, ignoreCourseLecturerViolationQuery, courseID)
	if err != nil {
		var pgErr *pgconn.PgError
		switch {
		case errors.As(err, &pgErr) && pgErr.Code == "23505": // Unique constraint violation / already ignored
			w.WriteHeader(http.StatusConflict)
		case errors.As(err, &pgErr) && pgErr.Code == "23503": // Foreign key violation / course does not exist
			w.WriteHeader(http.StatusNotFound)
		default:
			logError(r, err)
			w.WriteHeader(http.StatusInternalServerError)
		}
		return
	}

	w.WriteHeader(http.StatusCreated)
}

// Handler: DELETE /course-lecturer-violations/ignore; Unignore the lecturer violation of a course
func (s *Server) unignoreCourseLecturerViolation(w http.ResponseWriter, r *http.Request) {
	courseID, ok := readIgnoreCourseLecturerViolationRequest(w, r)
	if !ok {
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	result, err := s.DB.Exec(ctx, unignoreCourseLecturerViolationQuery, courseID)
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

// Handler: GET /course-lecturer-violations/ignored; Get all courses whose lecturer violation is ignored
func (s *Server) getIgnoredCourseLecturerViolations(w http.ResponseWriter, r *http.Request) {
	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	// Delete irrelevant ignores (already solved)
	if _, err := s.DB.Exec(ctx, deleteSolvedIgnoredCourseLecturerViolationsQuery); err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	rows, err := s.DB.Query(ctx, getIgnoredCourseLecturerViolationsQuery)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	ignored, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (IgnoredCourseLecturerViolation, error) {
		var i IgnoredCourseLecturerViolation
		err := row.Scan(&i.CourseID)
		return i, err
	})
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if ignored == nil {
		ignored = []IgnoredCourseLecturerViolation{}
	}

	writeJSON(w, http.StatusOK, ignored)
}
