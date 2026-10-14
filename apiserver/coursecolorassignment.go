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

func registerCourseColorAssignmentRoutes(s *Server, mux *http.ServeMux) {
	mux.HandleFunc("POST /course/{course_id}/color/{color_id}", s.createCourseColorAssignment)
	mux.HandleFunc("DELETE /course/{course_id}/color/{color_id}", s.deleteCourseColorAssignment)
	mux.HandleFunc("GET /course/{course_id}/colors", s.getCourseCourseColorAssignments)
	mux.HandleFunc("GET /course/colors", s.getCourseColorAssignments)
	// Prevent PUT/DELETE from matching /course/{id} wildcard routes
	mux.HandleFunc("PUT /course/colors", methodNotAllowed)
	mux.HandleFunc("DELETE /course/colors", methodNotAllowed)
}

// CourseColorBasicData contains the basic fields of a course color.
type CourseColorBasicData struct {
	Name *string `json:"name,omitempty"`
	Hex  *string `json:"hex,omitempty"`
}

// CourseColor is the representation of a row in the 'course_color' database table.
type CourseColor struct {
	ID int `json:"id"`
	CourseColorBasicData
}

//go:embed queries/create_course_color_assignment.sql
var createCourseColorAssignmentQuery string

// Handler: POST /course/{course_id}/color/{color_id}; Assign a color to a course
func (s *Server) createCourseColorAssignment(w http.ResponseWriter, r *http.Request) {
	courseIDStr := r.PathValue("course_id")
	courseID, err := parseID(courseIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	colorIDStr := r.PathValue("color_id")
	colorID, err := parseID(colorIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	_, err = s.DB.Exec(ctx, createCourseColorAssignmentQuery, courseID, colorID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // Unique constraint violation
			logRequest(r, "Course-color assignment already exists")
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

	sendUpdate(updateInfo{Resource: "courseColorAssignment", Operation: createOperation, Data: CourseColorAssignment{Course: courseID, Color: colorID}})

	w.WriteHeader(http.StatusCreated)
}

//go:embed queries/delete_course_color_assignment.sql
var deleteCourseColorAssignmentQuery string

// Handler: DELETE /course/{course_id}/color/{color_id}; Remove a color from a course
func (s *Server) deleteCourseColorAssignment(w http.ResponseWriter, r *http.Request) {
	courseIDStr := r.PathValue("course_id")
	courseID, err := parseID(courseIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	colorIDStr := r.PathValue("color_id")
	colorID, err := parseID(colorIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	result, err := s.DB.Exec(ctx, deleteCourseColorAssignmentQuery, courseID, colorID)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if result.RowsAffected() == 0 {
		logRequest(r, "Course-color assignment not found")
		w.WriteHeader(http.StatusNotFound)
		return
	}

	sendUpdate(updateInfo{Resource: "courseColorAssignment", Operation: deletionOperation, Data: CourseColorAssignment{Course: courseID, Color: colorID}})

	w.WriteHeader(http.StatusOK)
}

//go:embed queries/get_course_color_assignments.sql
var getCourseCourseColorsQuery string

// Handler: GET /course/{course_id}/colors; Get all colors assigned to a single course
func (s *Server) getCourseCourseColorAssignments(w http.ResponseWriter, r *http.Request) {
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
	rows, err := s.DB.Query(ctx, getCourseCourseColorsQuery, courseID)
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
	err = rows.Scan(&emptyCheck, nil, nil)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if emptyCheck == nil {
		writeJSON(w, http.StatusOK, []CourseColor{})
		return
	}

	var colors []CourseColor
	for {
		var color CourseColor
		err := rows.Scan(
			&color.ID,
			&color.Name,
			&color.Hex,
		)
		if err != nil {
			logError(r, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		colors = append(colors, color)

		if !rows.Next() {
			break
		}
	}

	if err = rows.Err(); err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, colors)
}

type CourseColorAssignment struct {
	Course int `json:"course"`
	Color  int `json:"color"`
}

//go:embed queries/gg/course_color_assignments.sql
var getCourseColorAssignmentsQuery string

func (s *Server) fetchCourseColorAssignments(ctx context.Context) ([]CourseColorAssignment, error) {
	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	rows, err := s.DB.Query(ctx, getCourseColorAssignmentsQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var courseColorAssignments []CourseColorAssignment
	for rows.Next() {
		var courseColorAssignment CourseColorAssignment
		err := rows.Scan(
			&courseColorAssignment.Course,
			&courseColorAssignment.Color,
		)
		if err != nil {
			return nil, err
		}
		courseColorAssignments = append(courseColorAssignments, courseColorAssignment)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	// Return empty array instead of null if no course color assingnments found
	if courseColorAssignments == nil {
		courseColorAssignments = []CourseColorAssignment{}
	}

	return courseColorAssignments, nil
}

// Handler: GET /course/colors; Get all course-color assignments
func (s *Server) getCourseColorAssignments(w http.ResponseWriter, r *http.Request) {
	courseColorAssignments, err := s.fetchCourseColorAssignments(r.Context())

	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, courseColorAssignments)
}
