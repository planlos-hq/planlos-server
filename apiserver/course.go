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

	"github.com/jackc/pgx/v5/pgconn"
)

func registerCourseRoutes(s *Server, mux *http.ServeMux) {
	mux.HandleFunc("GET /course/{id}", s.getCourse)
	createHandler := requireContentType("application/json", true)(http.HandlerFunc(s.createCourse))
	mux.Handle("POST /course", createHandler)
	mux.HandleFunc("DELETE /course/{id}", s.deleteCourse)
	putHandler := requireContentType("application/json", true)(http.HandlerFunc(s.putCourse))
	mux.Handle("PUT /course/{id}", putHandler)
	mux.HandleFunc("GET /course", s.getCourses)
}

type CourseBasicData struct {
	Number       *string          `json:"number,omitempty"`
	Name         *string          `json:"name,omitempty"`
	SWS          *float64         `json:"sws,omitempty"`
	CreditPoints *float64         `json:"credit_points,omitempty"`
	Abbreviation *string          `json:"abbreviation,omitempty"`
	ExtraData    *json.RawMessage `json:"extra_data,omitempty"`
}

// Course is the representation of a row in the 'course' database table.
type Course struct {
	ID int `json:"id"`
	CourseBasicData
}

// CreateCourseRequest is the representation of the expected JSON body for the POST /course endpoint.
type CreateCourseRequest CourseBasicData

// PatchCourseRequest is the representation of the expected JSON body for the PUT /course endpoint.
type PatchCourseRequest CreateCourseRequest

//go:embed queries/get_course.sql
var getCourseQuery string

// Handler: GET /course/{id}; Get a single course by ID
func (s *Server) getCourse(w http.ResponseWriter, r *http.Request) {
	courseIDStr := r.PathValue("id")
	courseID, err := parseID(courseIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	var course Course

	err = s.DB.QueryRow(ctx, getCourseQuery, courseID).Scan(&course.ID, &course.Number, &course.Name, &course.SWS, &course.CreditPoints, &course.Abbreviation, &course.ExtraData)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			logRequest(r, "Course not found: "+courseIDStr)
			w.WriteHeader(http.StatusNotFound)
		} else {
			logError(r, err)
			w.WriteHeader(http.StatusInternalServerError)
		}
		return
	}

	writeJSON(w, http.StatusOK, course)
}

//go:embed queries/create_course.sql
var createCourseQuery string

// Handler: POST /course; Create a new course
func (s *Server) createCourse(w http.ResponseWriter, r *http.Request) {
	var request CreateCourseRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		logError(r, err)
		return
	}

	if request.Number == nil || request.Name == nil || request.Abbreviation == nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	// default empty extra data
	if request.ExtraData == nil {
		extraData := json.RawMessage([]byte("{}"))
		request.ExtraData = &extraData
	}

	var newCourse Course
	err := s.DB.QueryRow(ctx, createCourseQuery, request.Number, request.Name, request.SWS, request.CreditPoints, request.Abbreviation, request.ExtraData).Scan(&newCourse.ID, &newCourse.Number, &newCourse.Name, &newCourse.SWS, &newCourse.CreditPoints, &newCourse.Abbreviation, &newCourse.ExtraData)

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		logError(r, err)
		return
	}

	sendUpdate(updateInfo{Resource: "course", Operation: createOperation, Data: newCourse})

	writeJSON(w, http.StatusCreated, newCourse)
}

//go:embed queries/delete_course.sql
var deleteCourseQuery string

// Handler: DELETE /course/{id}; Delete a course
func (s *Server) deleteCourse(w http.ResponseWriter, r *http.Request) {
	courseIDStr := r.PathValue("id")
	courseID, err := parseID(courseIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	result, err := s.DB.Exec(ctx, deleteCourseQuery, courseID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			logRequest(r, fmt.Sprintf("Cannot delete: still referenced by %s", pgErr.ConstraintName))
			w.WriteHeader(http.StatusConflict)
		} else {
			logError(r, err)
			w.WriteHeader(http.StatusInternalServerError)
		}
		return
	}

	if result.RowsAffected() == 0 {
		logRequest(r, "Course not found: "+courseIDStr)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	sendUpdate(updateInfo{Resource: "course", Operation: deletionOperation, Data: courseID})

	w.WriteHeader(http.StatusOK)
}

//go:embed queries/update_course.sql
var updateCourseQuery string

// Handler: PUT /course/{id}; Update an existing course
func (s *Server) putCourse(w http.ResponseWriter, r *http.Request) {
	courseIDStr := r.PathValue("id")
	courseID, err := parseID(courseIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	var request PatchCourseRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	var updatedCourse Course
	err = s.DB.QueryRow(ctx, updateCourseQuery, request.Number, request.Name, request.SWS, request.CreditPoints, request.Abbreviation, request.ExtraData, courseID).Scan(&updatedCourse.ID, &updatedCourse.Number, &updatedCourse.Name, &updatedCourse.SWS, &updatedCourse.CreditPoints, &updatedCourse.Abbreviation, &updatedCourse.ExtraData)

	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	sendUpdate(updateInfo{Resource: "course", Operation: updateOperation, Data: updatedCourse})

	writeJSON(w, http.StatusOK, updatedCourse)
}

//go:embed queries/gg/courses.sql
var getCoursesQuery string

func (s *Server) fetchCourses(ctx context.Context) ([]Course, error) {
	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	rows, err := s.DB.Query(ctx, getCoursesQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var courses []Course
	for rows.Next() {
		var course Course
		err := rows.Scan(
			&course.ID,
			&course.Number,
			&course.Name,
			&course.SWS,
			&course.CreditPoints,
			&course.Abbreviation,
			&course.ExtraData,
		)
		if err != nil {
			return nil, err
		}
		courses = append(courses, course)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	// Return empty array instead of null if no courses found
	if courses == nil {
		courses = []Course{}
	}

	return courses, nil
}

// Handler: GET /course; Get all courses
func (s *Server) getCourses(w http.ResponseWriter, r *http.Request) {
	courses, err := s.fetchCourses(r.Context())
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, courses)
}
