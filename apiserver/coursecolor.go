package apiserver

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func registerCourseColorRoutes(s *Server, mux *http.ServeMux) {
	mux.HandleFunc("GET /course-color/{id}", s.getCourseColor)
	createHandler := requireContentType("application/json", true)(http.HandlerFunc(s.createCourseColor))
	mux.Handle("POST /course-color", createHandler)
	mux.HandleFunc("DELETE /course-color/{id}", s.deleteCourseColor)
	putHandler := requireContentType("application/json", true)(http.HandlerFunc(s.putCourseColor))
	mux.Handle("PUT /course-color/{id}", putHandler)
	mux.HandleFunc("GET /course-color", s.getCourseColors)
}

// CreateCourseColorRequest is the representation of the expected JSON body for the POST /course-color endpoint.
type CreateCourseColorRequest CourseColorBasicData

// PatchCourseColorRequest is the representation of the expected JSON body for the PUT /course-color endpoint.
type PatchCourseColorRequest CreateCourseColorRequest

//go:embed queries/get_course_color.sql
var getCourseColorQuery string

// Handler: GET /course-color/{id}; Get a single course color by ID
func (s *Server) getCourseColor(w http.ResponseWriter, r *http.Request) {
	colorIDStr := r.PathValue("id")
	colorID, err := parseID(colorIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	var color CourseColor

	err = s.DB.QueryRow(ctx, getCourseColorQuery, colorID).Scan(
		&color.ID,
		&color.Name,
		&color.Hex,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			logRequest(r, "Course color not found: "+colorIDStr)
			w.WriteHeader(http.StatusNotFound)
		} else {
			logError(r, err)
			w.WriteHeader(http.StatusInternalServerError)
		}
		return
	}

	writeJSON(w, http.StatusOK, color)
}

//go:embed queries/create_course_color.sql
var createCourseColorQuery string

var hexPattern = regexp.MustCompile(`^#[A-Fa-f0-9]{6}$`)

// Handler: POST /course-color; Create a new course color
func (s *Server) createCourseColor(w http.ResponseWriter, r *http.Request) {
	var request CreateCourseColorRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		logError(r, err)
		return
	}

	if request.Name == nil || request.Hex == nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Validate hex format
	if !hexPattern.MatchString(*request.Hex) {
		logRequest(r, "Invalid hex format: "+*request.Hex)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	var newColor CourseColor
	err := s.DB.QueryRow(ctx, createCourseColorQuery,
		request.Name,
		request.Hex,
	).Scan(
		&newColor.ID,
		&newColor.Name,
		&newColor.Hex,
	)

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		logError(r, err)
		return
	}

	sendUpdate(updateInfo{Resource: "courseColor", Operation: createOperation, Data: newColor})

	writeJSON(w, http.StatusCreated, newColor)
}

//go:embed queries/delete_course_color.sql
var deleteCourseColorQuery string

// Handler: DELETE /course-color/{id}; Delete a course color
func (s *Server) deleteCourseColor(w http.ResponseWriter, r *http.Request) {
	colorIDStr := r.PathValue("id")
	colorID, err := parseID(colorIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	result, err := s.DB.Exec(ctx, deleteCourseColorQuery, colorID)
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
		logRequest(r, "Course color not found: "+colorIDStr)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	sendUpdate(updateInfo{Resource: "courseColor", Operation: deletionOperation, Data: colorID})

	w.WriteHeader(http.StatusOK)
}

//go:embed queries/update_course_color.sql
var updateCourseColorQuery string

// Handler: PUT /course-color/{id}; Update an existing course color
func (s *Server) putCourseColor(w http.ResponseWriter, r *http.Request) {
	colorIDStr := r.PathValue("id")
	colorID, err := parseID(colorIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	var request PatchCourseColorRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Validate hex format if provided
	if request.Hex != nil && !hexPattern.MatchString(*request.Hex) {
		logRequest(r, "Invalid hex format: "+*request.Hex)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	var updatedColor CourseColor
	err = s.DB.QueryRow(ctx, updateCourseColorQuery,
		request.Name,
		request.Hex,
		colorID,
	).Scan(
		&updatedColor.ID,
		&updatedColor.Name,
		&updatedColor.Hex,
	)

	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	sendUpdate(updateInfo{Resource: "courseColor", Operation: updateOperation, Data: updatedColor})

	writeJSON(w, http.StatusOK, updatedColor)
}

//go:embed queries/gg/course_colors.sql
var getCourseColorsQuery string

func (s *Server) fetchCourseColors(ctx context.Context) ([]CourseColor, error) {
	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	rows, err := s.DB.Query(ctx, getCourseColorsQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var courseColors []CourseColor
	for rows.Next() {
		var courseColor CourseColor
		err := rows.Scan(
			&courseColor.ID,
			&courseColor.Name,
			&courseColor.Hex,
		)
		if err != nil {
			return nil, err
		}
		courseColors = append(courseColors, courseColor)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	// Return empty array instead of null if no course colors found
	if courseColors == nil {
		courseColors = []CourseColor{}
	}

	return courseColors, nil
}

// Handler: GET /course-color; Get all course colors
func (s *Server) getCourseColors(w http.ResponseWriter, r *http.Request) {
	courseColors, err := s.fetchCourseColors(r.Context())

	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, courseColors)
}
