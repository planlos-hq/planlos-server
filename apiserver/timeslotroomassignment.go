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

func registerTimeSlotRoomAssignmentRoutes(s *Server, mux *http.ServeMux) {
	mux.HandleFunc("POST /course-time-slot/{time_slot_id}/room/{room_id}", s.createTimeSlotRoomAssignment)
	mux.HandleFunc("DELETE /course-time-slot/{time_slot_id}/room/{room_id}", s.deleteTimeSlotRoomAssignment)
	mux.HandleFunc("GET /course-time-slot/{time_slot_id}/rooms", s.getTimeSlotRoomAssignments)
	mux.HandleFunc("GET /course-time-slot/rooms", s.getTimeSlotRoomAssignmentsAll)
	// Prevent PUT/DELETE from matching /course-time-slot/{id} wildcard routes
	mux.HandleFunc("PUT /course-time-slot/rooms", methodNotAllowed)
	mux.HandleFunc("DELETE /course-time-slot/rooms", methodNotAllowed)
}

//go:embed queries/create_time_slot_room_assignment.sql
var createTimeSlotRoomAssignmentQuery string

// Handler: POST /course-time-slot/{time_slot_id}/room/{room_id}; Assign a room to a course time slot
func (s *Server) createTimeSlotRoomAssignment(w http.ResponseWriter, r *http.Request) {
	timeSlotIDStr := r.PathValue("time_slot_id")
	timeSlotID, err := parseID(timeSlotIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	roomIDStr := r.PathValue("room_id")
	roomID, err := parseID(roomIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	_, err = s.DB.Exec(ctx, createTimeSlotRoomAssignmentQuery, timeSlotID, roomID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // Unique constraint violation
			logRequest(r, "Time slot-room assignment already exists")
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

	sendUpdate(updateInfo{Resource: "timeSlotRoomAssignment", Operation: createOperation, Data: TimeSlotRoomAssignment{TimeSlot: timeSlotID, Room: roomID}})

	w.WriteHeader(http.StatusCreated)
}

//go:embed queries/delete_time_slot_room_assignment.sql
var deleteTimeSlotRoomAssignmentQuery string

// Handler: DELETE /course-time-slot/{time_slot_id}/room/{room_id}; Remove a room from a course time slot
func (s *Server) deleteTimeSlotRoomAssignment(w http.ResponseWriter, r *http.Request) {
	timeSlotIDStr := r.PathValue("time_slot_id")
	timeSlotID, err := parseID(timeSlotIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	roomIDStr := r.PathValue("room_id")
	roomID, err := parseID(roomIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	result, err := s.DB.Exec(ctx, deleteTimeSlotRoomAssignmentQuery, timeSlotID, roomID)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if result.RowsAffected() == 0 {
		logRequest(r, "Time slot-room assignment not found")
		w.WriteHeader(http.StatusNotFound)
		return
	}

	sendUpdate(updateInfo{Resource: "timeSlotRoomAssignment", Operation: deletionOperation, Data: TimeSlotRoomAssignment{TimeSlot: timeSlotID, Room: roomID}})

	w.WriteHeader(http.StatusOK)
}

//go:embed queries/get_time_slot_room_assignments.sql
var getTimeSlotRoomAssignmentsQuery string

// Handler: GET /course-time-slot/{time_slot_id}/rooms; Get all rooms assigned to a single course time slot
func (s *Server) getTimeSlotRoomAssignments(w http.ResponseWriter, r *http.Request) {
	timeSlotIDStr := r.PathValue("time_slot_id")
	timeSlotID, err := parseID(timeSlotIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	rows, err := s.DB.Query(ctx, getTimeSlotRoomAssignmentsQuery, timeSlotID)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	if !rows.Next() {
		logRequest(r, "Time slot not found: "+timeSlotIDStr)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	var emptyCheck *int
	err = rows.Scan(&emptyCheck, nil, nil, nil)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if emptyCheck == nil {
		writeJSON(w, http.StatusOK, []Room{})
		return
	}

	var rooms []Room
	for {
		var room Room
		err := rows.Scan(
			&room.ID,
			&room.Number,
			&room.Name,
			&room.Seats,
		)
		if err != nil {
			logError(r, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		rooms = append(rooms, room)

		if !rows.Next() {
			break
		}
	}

	if err = rows.Err(); err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, rooms)
}

type TimeSlotRoomAssignment struct {
	TimeSlot int `json:"time_slot"`
	Room     int `json:"room"`
}

//go:embed queries/gg/time_slot_room_assignments.sql
var getTimeSlotRoomAssignmentsAllQuery string

func (s *Server) fetchTimeSlotRoomAssignments(ctx context.Context) ([]TimeSlotRoomAssignment, error) {
	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	rows, err := s.DB.Query(ctx, getTimeSlotRoomAssignmentsAllQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var assignments []TimeSlotRoomAssignment
	for rows.Next() {
		var assignment TimeSlotRoomAssignment
		err := rows.Scan(
			&assignment.TimeSlot,
			&assignment.Room,
		)
		if err != nil {
			return nil, err
		}
		assignments = append(assignments, assignment)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	// Return empty array instead of null if no assignments found
	if assignments == nil {
		assignments = []TimeSlotRoomAssignment{}
	}

	return assignments, nil
}

// Handler: GET /course-time-slot/rooms; Get all time-slot-room assignments
func (s *Server) getTimeSlotRoomAssignmentsAll(w http.ResponseWriter, r *http.Request) {
	assignments, err := s.fetchTimeSlotRoomAssignments(r.Context())

	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, assignments)
}
