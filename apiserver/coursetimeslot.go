package apiserver

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func registerCourseTimeSlotRoutes(s *Server, mux *http.ServeMux) {
	mux.HandleFunc("GET /course-time-slot/{id}", s.getCourseTimeSlot)
	createHandler := requireContentType("application/json", true)(http.HandlerFunc(s.createCourseTimeSlot))
	mux.Handle("POST /course-time-slot", createHandler)
	mux.HandleFunc("DELETE /course-time-slot/{id}", s.deleteCourseTimeSlot)
	putHandler := requireContentType("application/json", true)(http.HandlerFunc(s.putCourseTimeSlot))
	mux.Handle("PUT /course-time-slot/{id}", putHandler)
	mux.HandleFunc("GET /course-time-slot", s.getCourseTimeSlots)
}

// CourseTimeSlotBasicData contains the basic fields of a course time slot.
type CourseTimeSlotBasicData struct {
	Course     *int    `json:"course,omitempty"`
	Weekday    *int    `json:"weekday,omitempty"`
	At         *string `json:"at,omitempty"`
	Duration   *string `json:"duration,omitempty"`
	OneOffDate *string `json:"one_off_date,omitempty"`
}

// CourseTimeSlot is the representation of a row in the 'course_time_slots' database table.
type CourseTimeSlot struct {
	ID int `json:"id"`
	CourseTimeSlotBasicData
}

// CreateCourseTimeSlotRequest is the representation of the expected JSON body for the POST /course-time-slot endpoint.
type CreateCourseTimeSlotRequest CourseTimeSlotBasicData

// PatchCourseTimeSlotRequest is the representation of the expected JSON body for the PUT /course-time-slot endpoint.
type PatchCourseTimeSlotRequest CreateCourseTimeSlotRequest

//go:embed queries/get_course_time_slot.sql
var getCourseTimeSlotQuery string

// Handler: GET /course-time-slot/{id}; Get a single course time slot by ID
func (s *Server) getCourseTimeSlot(w http.ResponseWriter, r *http.Request) {
	timeSlotIDStr := r.PathValue("id")
	timeSlotID, err := parseID(timeSlotIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	var timeSlot CourseTimeSlot

	err = s.DB.QueryRow(ctx, getCourseTimeSlotQuery, timeSlotID).Scan(
		&timeSlot.ID,
		&timeSlot.Course,
		&timeSlot.Weekday,
		&timeSlot.At,
		&timeSlot.Duration,
		&timeSlot.OneOffDate,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			logRequest(r, "Course time slot not found: "+timeSlotIDStr)
			w.WriteHeader(http.StatusNotFound)
		} else {
			logError(r, err)
			w.WriteHeader(http.StatusInternalServerError)
		}
		return
	}

	writeJSON(w, http.StatusOK, timeSlot)
}

//go:embed queries/create_course_time_slot.sql
var createCourseTimeSlotQuery string

// Handler: POST /course-time-slot; Create a new course time slot
func (s *Server) createCourseTimeSlot(w http.ResponseWriter, r *http.Request) {
	var request CreateCourseTimeSlotRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		logError(r, err)
		return
	}

	if request.Course == nil || request.Weekday == nil || request.At == nil || request.Duration == nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Validate weekday
	if *request.Weekday < 0 || *request.Weekday > 6 {
		logRequest(r, "Invalid weekday: "+strconv.Itoa(*request.Weekday))
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	var newTimeSlot CourseTimeSlot
	err := s.DB.QueryRow(ctx, createCourseTimeSlotQuery,
		request.Course,
		request.Weekday,
		request.At,
		request.Duration,
		request.OneOffDate,
	).Scan(
		&newTimeSlot.ID,
		&newTimeSlot.Course,
		&newTimeSlot.Weekday,
		&newTimeSlot.At,
		&newTimeSlot.Duration,
		&newTimeSlot.OneOffDate,
	)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // Unique constraint violation
			logRequest(r, "Timeslot already exists")
			w.WriteHeader(http.StatusConflict)
		} else if pgErr != nil && pgErr.Code == "23514" { // Check constraint violation
			w.WriteHeader(http.StatusBadRequest)
		} else if pgErr != nil && pgErr.Code == "23503" { // Foreign key violation
			logRequest(r, fmt.Sprintf("%s violated; invalid reference", pgErr.ConstraintName))
			w.WriteHeader(http.StatusBadRequest)
		} else {
			logError(r, err)
			w.WriteHeader(http.StatusInternalServerError)
		}
		return
	}

	sendUpdate(updateInfo{Resource: "courseTimeSlot", Operation: createOperation, Data: newTimeSlot})

	writeJSON(w, http.StatusCreated, newTimeSlot)
}

//go:embed queries/delete_course_time_slot.sql
var deleteCourseTimeSlotQuery string

// Handler: DELETE /course-time-slot/{id}; Delete a course time slot
func (s *Server) deleteCourseTimeSlot(w http.ResponseWriter, r *http.Request) {
	timeSlotIDStr := r.PathValue("id")
	timeSlotID, err := parseID(timeSlotIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	result, err := s.DB.Exec(ctx, deleteCourseTimeSlotQuery, timeSlotID)
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
		logRequest(r, "Course time slot not found: "+timeSlotIDStr)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	sendUpdate(updateInfo{Resource: "courseTimeSlot", Operation: deletionOperation, Data: timeSlotID})

	w.WriteHeader(http.StatusOK)
}

//go:embed queries/update_course_time_slot.sql
var updateCourseTimeSlotQuery string

// Handler: PUT /course-time-slot/{id}; Update an existing course time slot
func (s *Server) putCourseTimeSlot(w http.ResponseWriter, r *http.Request) {
	timeSlotIDStr := r.PathValue("id")
	timeSlotID, err := parseID(timeSlotIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	var request PatchCourseTimeSlotRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Validate weekday if provided
	if request.Weekday != nil && (*request.Weekday < 0 || *request.Weekday > 6) {
		logRequest(r, "Invalid weekday: "+strconv.Itoa(*request.Weekday))
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	var updatedTimeSlot CourseTimeSlot
	err = s.DB.QueryRow(ctx, updateCourseTimeSlotQuery,
		request.Course,
		request.Weekday,
		request.At,
		request.Duration,
		request.OneOffDate,
		timeSlotID,
	).Scan(
		&updatedTimeSlot.ID,
		&updatedTimeSlot.Course,
		&updatedTimeSlot.Weekday,
		&updatedTimeSlot.At,
		&updatedTimeSlot.Duration,
		&updatedTimeSlot.OneOffDate,
	)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" { // Foreign key violation
			logRequest(r, fmt.Sprintf("%s violated; invalid reference", pgErr.ConstraintName))
			w.WriteHeader(http.StatusBadRequest)
		} else if pgErr != nil && pgErr.Code == "23514" { // Check constraint violation
			w.WriteHeader(http.StatusBadRequest)
		} else {
			logError(r, err)
			w.WriteHeader(http.StatusInternalServerError)
		}
		return
	}

	sendUpdate(updateInfo{Resource: "courseTimeSlot", Operation: updateOperation, Data: updatedTimeSlot})

	writeJSON(w, http.StatusOK, updatedTimeSlot)
}

//go:embed queries/gg/course_time_slots.sql
var getCourseTimeSlotsQuery string

func (s *Server) fetchCourseTimeSlots(ctx context.Context) ([]CourseTimeSlot, error) {
	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	rows, err := s.DB.Query(ctx, getCourseTimeSlotsQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var courseTimeSlots []CourseTimeSlot
	for rows.Next() {
		var courseTimeSlot CourseTimeSlot
		err := rows.Scan(
			&courseTimeSlot.ID,
			&courseTimeSlot.Course,
			&courseTimeSlot.Weekday,
			&courseTimeSlot.At,
			&courseTimeSlot.Duration,
			&courseTimeSlot.OneOffDate,
		)
		if err != nil {
			return nil, err
		}
		courseTimeSlots = append(courseTimeSlots, courseTimeSlot)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	// Return empty array instead of null if no course time slots found
	if courseTimeSlots == nil {
		courseTimeSlots = []CourseTimeSlot{}
	}

	return courseTimeSlots, nil
}

// Handler: GET /course-time-slot; Get all course time slots
func (s *Server) getCourseTimeSlots(w http.ResponseWriter, r *http.Request) {
	courseTimeSlots, err := s.fetchCourseTimeSlots(r.Context())

	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, courseTimeSlots)
}
