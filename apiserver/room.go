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

func registerRoomRoutes(s *Server, mux *http.ServeMux) {
	mux.HandleFunc("GET /room/{id}", s.getRoom)
	createHandler := requireContentType("application/json", true)(http.HandlerFunc(s.createRoom))
	mux.Handle("POST /room", createHandler)
	mux.HandleFunc("DELETE /room/{id}", s.deleteRoom)
	putHandler := requireContentType("application/json", true)(http.HandlerFunc(s.putRoom))
	mux.Handle("PUT /room/{id}", putHandler)
	mux.HandleFunc("GET /room", s.getRooms)
}

type RoomBasicData struct {
	Number    *string          `json:"number,omitempty"`
	Name      *string          `json:"name,omitempty"`
	Seats     *int             `json:"seats,omitempty"`
	ExtraData *json.RawMessage `json:"extra_data,omitempty"`
}

// Room is the representation of a row in the 'room' database table.
type Room struct {
	ID int `json:"id"`
	RoomBasicData
}

// CreateRoomRequest is the representation of the expected JSON body for the POST /room endpoint.
type CreateRoomRequest RoomBasicData

// PatchRoomRequest is the representation of the expected JSON body for the PUT /room endpoint.
type PatchRoomRequest RoomBasicData

//go:embed queries/get_room.sql
var getRoomQuery string

// Handler: GET /room/{id}; Get a single room by ID
func (s *Server) getRoom(w http.ResponseWriter, r *http.Request) {
	roomIDStr := r.PathValue("id")
	roomID, err := parseID(roomIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	var room Room

	err = s.DB.QueryRow(ctx, getRoomQuery, roomID).Scan(&room.ID, &room.Number, &room.Name, &room.Seats, &room.ExtraData)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			logRequest(r, "Room not found: "+roomIDStr)
			w.WriteHeader(http.StatusNotFound)
		} else {
			logError(r, err)
			w.WriteHeader(http.StatusInternalServerError)
		}
		return
	}

	writeJSON(w, http.StatusOK, room)
}

//go:embed queries/create_room.sql
var createRoomQuery string

// Handler: POST /room; Create a new room
func (s *Server) createRoom(w http.ResponseWriter, r *http.Request) {
	var request CreateRoomRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		logError(r, err)
		return
	}

	if request.Number == nil {
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

	var newRoom Room
	err := s.DB.QueryRow(ctx, createRoomQuery, request.Number, request.Name, request.Seats, request.ExtraData).Scan(&newRoom.ID, &newRoom.Number, &newRoom.Name, &newRoom.Seats, &newRoom.ExtraData)

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		logError(r, err)
		return
	}

	sendUpdate(updateInfo{Resource: "room", Operation: createOperation, Data: newRoom})

	writeJSON(w, http.StatusCreated, newRoom)
}

//go:embed queries/delete_room.sql
var deleteRoomQuery string

// Handler: DELETE /room/{id}; Delete a room
func (s *Server) deleteRoom(w http.ResponseWriter, r *http.Request) {
	roomIDStr := r.PathValue("id")
	roomID, err := parseID(roomIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	result, err := s.DB.Exec(ctx, deleteRoomQuery, roomID)
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
		logRequest(r, "Room not found: "+roomIDStr)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	sendUpdate(updateInfo{Resource: "room", Operation: deletionOperation, Data: roomID})

	w.WriteHeader(http.StatusOK)
}

//go:embed queries/update_room.sql
var updateRoomQuery string

// Handler: PUT /room/{id}; Update an existing room
func (s *Server) putRoom(w http.ResponseWriter, r *http.Request) {
	roomIDStr := r.PathValue("id")
	roomID, err := parseID(roomIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	var request PatchRoomRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	var updatedRoom Room
	err = s.DB.QueryRow(ctx, updateRoomQuery, request.Number, request.Name, request.Seats, request.ExtraData, roomID).Scan(&updatedRoom.ID, &updatedRoom.Number, &updatedRoom.Name, &updatedRoom.Seats, &updatedRoom.ExtraData)

	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	sendUpdate(updateInfo{Resource: "room", Operation: updateOperation, Data: updatedRoom})

	writeJSON(w, http.StatusOK, updatedRoom)
}

//go:embed queries/gg/rooms.sql
var getRoomsQuery string

func (s *Server) fetchRooms(ctx context.Context) ([]Room, error) {
	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	rows, err := s.DB.Query(ctx, getRoomsQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rooms []Room
	for rows.Next() {
		var room Room
		err := rows.Scan(
			&room.ID,
			&room.Number,
			&room.Name,
			&room.Seats,
			&room.ExtraData,
		)
		if err != nil {
			return nil, err
		}
		rooms = append(rooms, room)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	// Return empty array instead of null if no rooms found
	if rooms == nil {
		rooms = []Room{}
	}

	return rooms, nil
}

// Handler: GET /room; Get all rooms
func (s *Server) getRooms(w http.ResponseWriter, r *http.Request) {
	rooms, err := s.fetchRooms(r.Context())

	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, rooms)
}
