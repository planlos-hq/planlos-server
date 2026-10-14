package apiserver

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func registerCourseLecturerRoutes(s *Server, mux *http.ServeMux) {
	mux.HandleFunc("POST /course/{course_id}/lecturer/{lecturer_id}", s.createCourseLecturer)
	mux.HandleFunc("DELETE /course/{course_id}/lecturer/{lecturer_id}", s.deleteCourseLecturer)
	mux.HandleFunc("GET /course/{course_id}/lecturers", s.getCourseCourseLecturers)
	mux.HandleFunc("GET /course/lecturers", s.getCourseLecturers)
	// Prevent PUT/DELETE from matching /course/{id} wildcard routes
	mux.HandleFunc("PUT /course/lecturers", methodNotAllowed)
	mux.HandleFunc("DELETE /course/lecturers", methodNotAllowed)
}

//go:embed queries/create_course_lecturer.sql
var createCourseLecturerQuery string

// Handler: POST /course/{course_id}/lecturer/{lecturer_id}; Assign a lecturer to a course
func (s *Server) createCourseLecturer(w http.ResponseWriter, r *http.Request) {
	courseIDStr := r.PathValue("course_id")
	courseID, err := parseID(courseIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	lecturerIDStr := r.PathValue("lecturer_id")
	lecturerID, err := parseID(lecturerIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	_, err = s.DB.Exec(ctx, createCourseLecturerQuery, courseID, lecturerID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // Unique constraint violation
			logRequest(r, "Course-lecturer assignment already exists")
			w.WriteHeader(http.StatusConflict)
		} else if pgErr != nil && pgErr.Code == "23503" { // Foreign key violation
			logRequest(r, fmt.Sprintf("%s violated; invalid reference", pgErr.ConstraintName))
			w.WriteHeader(http.StatusNotFound)
		} else {
			logError(r, err)
			w.WriteHeader(http.StatusInternalServerError)
		}
		return
	}

	sendUpdate(updateInfo{Resource: "courseLecturer", Operation: createOperation, Data: CourseLecturer{Course: courseID, Lecturer: lecturerID}})

	w.WriteHeader(http.StatusCreated)
}

//go:embed queries/delete_course_lecturer.sql
var deleteCourseLecturerQuery string

// Handler: DELETE /course/{course_id}/lecturer/{lecturer_id}; Remove a lecturer from a course
func (s *Server) deleteCourseLecturer(w http.ResponseWriter, r *http.Request) {
	courseIDStr := r.PathValue("course_id")
	courseID, err := parseID(courseIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	lecturerIDStr := r.PathValue("lecturer_id")
	lecturerID, err := parseID(lecturerIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	result, err := s.DB.Exec(ctx, deleteCourseLecturerQuery, courseID, lecturerID)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if result.RowsAffected() == 0 {
		logRequest(r, "Course-lecturer assignment not found")
		w.WriteHeader(http.StatusNotFound)
		return
	}

	sendUpdate(updateInfo{Resource: "courseLecturer", Operation: deletionOperation, Data: CourseLecturer{Course: courseID, Lecturer: lecturerID}})

	w.WriteHeader(http.StatusOK)
}

//go:embed queries/get_course_lecturers.sql
var getCourseCourseLecturersQuery string

// Handler: GET /course/{course_id}/lecturers; Get all lecturers assigned to a single course
func (s *Server) getCourseCourseLecturers(w http.ResponseWriter, r *http.Request) {
	courseIDStr := r.PathValue("course_id")
	courseID, err := parseID(courseIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	// one row all nulls if no results but exists; no rows if not exist; normal otherwise
	rows, err := s.DB.Query(ctx, getCourseCourseLecturersQuery, courseID)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	if !rows.Next() {
		logRequest(r, "Course not found: "+courseIDStr)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	var emptyCheck *int
	// actual id never null
	err = rows.Scan(&emptyCheck, nil, nil, nil, nil, nil, nil)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if emptyCheck == nil {
		writeJSON(w, http.StatusOK, []Lecturer{})
		return
	}

	var lecturers []Lecturer
	for {
		var lecturer Lecturer
		err := rows.Scan(
			&lecturer.ID,
			&lecturer.FirstName,
			&lecturer.LastName,
			&lecturer.Abbreviation,
			&lecturer.Kind,
			&lecturer.Notes,
			&lecturer.IsRelevant,
		)
		if err != nil {
			logError(r, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		lecturers = append(lecturers, lecturer)

		if !rows.Next() {
			break
		}
	}

	if err = rows.Err(); err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, lecturers)
}

type CourseLecturer struct {
	Course   int `json:"course"`
	Lecturer int `json:"lecturer"`
}

//go:embed queries/gg/course_lecturers.sql
var getCourseLecturersQuery string

func (s *Server) fetchCourseLecturers(ctx context.Context) ([]CourseLecturer, error) {
	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	rows, err := s.DB.Query(ctx, getCourseLecturersQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var courseLecturers []CourseLecturer
	for rows.Next() {
		var courseLecturer CourseLecturer
		err := rows.Scan(
			&courseLecturer.Course,
			&courseLecturer.Lecturer,
		)
		if err != nil {
			return nil, err
		}
		courseLecturers = append(courseLecturers, courseLecturer)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	// Return empty array instead of null if no course lecturers found
	if courseLecturers == nil {
		courseLecturers = []CourseLecturer{}
	}

	return courseLecturers, nil
}

// Handler: GET /course/lecturers; Get all course-lecturer assignments
func (s *Server) getCourseLecturers(w http.ResponseWriter, r *http.Request) {
	courseLecturers, err := s.fetchCourseLecturers(r.Context())

	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, courseLecturers)
}
