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

func registerLecturerRoutes(s *Server, mux *http.ServeMux) {
	mux.HandleFunc("GET /lecturer/{id}", s.getLecturer)
	createHandler := requireContentType("application/json", true)(http.HandlerFunc(s.createLecturer))
	mux.Handle("POST /lecturer", createHandler)
	putHandler := requireContentType("application/json", true)(http.HandlerFunc(s.putLecturer))
	mux.Handle("PUT /lecturer/{id}", putHandler)
	mux.HandleFunc("GET /lecturer", s.getLecturers)
	mux.HandleFunc("DELETE /lecturer/{id}", s.deleteLecturer)
}

type LecturerBasicData struct {
	FirstName    *string          `json:"first_name,omitempty"`
	LastName     *string          `json:"last_name,omitempty"`
	Abbreviation *string          `json:"abbreviation,omitempty"`
	Kind         *string          `json:"kind,omitempty"`
	Notes        *string          `json:"notes,omitempty"`
	IsRelevant   *bool            `json:"is_relevant,omitempty"`
	ExtraData    *json.RawMessage `json:"extra_data,omitempty"`
	Title        *string          `json:"title,omitempty"`
}

// Lecturer is the representation of a row in the 'lecturer' database table
type Lecturer struct {
	ID int `json:"id"`
	LecturerBasicData
}

// CreateLecturerRequest is the representation of the expected JSON body for the POST /lecturer endpoint
type CreateLecturerRequest LecturerBasicData

// PatchRoomRequest is the representation of the expected JSON body for the PUT /lecturer endpoint.
type PatchLecturerRequest LecturerBasicData

//go:embed queries/get_lecturer.sql
var getLecturerQuery string

// Handler: GET /lecturer/{id}; Get a single lecturer by ID
func (s *Server) getLecturer(w http.ResponseWriter, r *http.Request) {
	lecturerIDStr := r.PathValue("id")
	lecturerID, err := parseID(lecturerIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	var lecturer Lecturer
	err = s.DB.QueryRow(ctx, getLecturerQuery, lecturerID).Scan(&lecturer.ID, &lecturer.FirstName, &lecturer.LastName, &lecturer.Abbreviation, &lecturer.Kind, &lecturer.Notes, &lecturer.IsRelevant, &lecturer.ExtraData, &lecturer.Title)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			logRequest(r, "Lecturer not found: "+lecturerIDStr)
			w.WriteHeader(http.StatusNotFound)
		} else {
			logError(r, err)
			w.WriteHeader(http.StatusInternalServerError)
		}
		return
	}

	writeJSON(w, http.StatusOK, lecturer)
}

//go:embed queries/create_lecturer.sql
var createLecturerQuery string

//go:embed queries/update_lecturer.sql
var updateLecturerQuery string

// Handler: PUT /lecturer/{id}; Update an existing lecturer
func (s *Server) putLecturer(w http.ResponseWriter, r *http.Request) {
	lecturerIDStr := r.PathValue("id")
	lecturerID, err := parseID(lecturerIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	var request PatchLecturerRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	var updatedLecturer Lecturer
	err = s.DB.QueryRow(ctx, updateLecturerQuery, request.FirstName, request.LastName, request.Abbreviation, request.Kind, request.Notes, request.IsRelevant, request.ExtraData, request.Title, lecturerID).Scan(&updatedLecturer.ID, &updatedLecturer.FirstName, &updatedLecturer.LastName, &updatedLecturer.Abbreviation, &updatedLecturer.Kind, &updatedLecturer.Notes, &updatedLecturer.IsRelevant, &updatedLecturer.ExtraData, &updatedLecturer.Title)

	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	sendUpdate(updateInfo{Resource: "lecturer", Operation: updateOperation, Data: updatedLecturer})

	writeJSON(w, http.StatusOK, updatedLecturer)
}

// Handler: POST /lecturer; Create a new lecturer
func (s *Server) createLecturer(w http.ResponseWriter, r *http.Request) {
	var request CreateLecturerRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		logError(r, err)
		return
	}

	if request.FirstName == nil || request.LastName == nil || request.Abbreviation == nil || request.Kind == nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	// Set default value for isRelevant if not provided
	isRelevant := true
	if request.IsRelevant != nil {
		isRelevant = *request.IsRelevant
	}

	// default empty extra data
	if request.ExtraData == nil {
		extraData := json.RawMessage([]byte("{}"))
		request.ExtraData = &extraData
	}

	var newLecturer Lecturer
	err := s.DB.QueryRow(ctx, createLecturerQuery, request.FirstName, request.LastName, request.Abbreviation, request.Kind, request.Notes, isRelevant, request.ExtraData, request.Title).Scan(&newLecturer.ID, &newLecturer.FirstName, &newLecturer.LastName, &newLecturer.Abbreviation, &newLecturer.Kind, &newLecturer.Notes, &newLecturer.IsRelevant, &newLecturer.ExtraData, &newLecturer.Title)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23514" { // Check constraint violation
			logRequest(r, "Invalid lecturer kind")
			w.WriteHeader(http.StatusBadRequest)
		} else {
			logError(r, err)
			w.WriteHeader(http.StatusInternalServerError)
		}
		return
	}

	sendUpdate(updateInfo{Resource: "lecturer", Operation: createOperation, Data: newLecturer})

	writeJSON(w, http.StatusCreated, newLecturer)
}

//go:embed queries/gg/lecturers.sql
var getLecturersQuery string

func (s *Server) fetchLecturers(ctx context.Context) ([]Lecturer, error) {
	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	rows, err := s.DB.Query(ctx, getLecturersQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lecturers []Lecturer
	for rows.Next() {
		var lecturer Lecturer
		err := rows.Scan(
			&lecturer.ID,
			&lecturer.FirstName,
			&lecturer.LastName,
			&lecturer.Abbreviation,
			&lecturer.Kind,
			&lecturer.Notes,
			&lecturer.IsRelevant,
			&lecturer.ExtraData,
			&lecturer.Title,
		)
		if err != nil {
			return nil, err
		}
		lecturers = append(lecturers, lecturer)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	// Return empty array instead of null if no lecturers found
	if lecturers == nil {
		lecturers = []Lecturer{}
	}

	return lecturers, nil
}

//go:embed queries/delete_lecturer.sql
var deleteLecturerQuery string

// Handler: DELETE /lecturer/{id}; Delete a lecturer
func (s *Server) deleteLecturer(w http.ResponseWriter, r *http.Request) {
	lecturerIDStr := r.PathValue("id")
	lecturerID, err := parseID(lecturerIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	result, err := s.DB.Exec(ctx, deleteLecturerQuery, lecturerID)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if result.RowsAffected() == 0 {
		logRequest(r, fmt.Sprintf("Lecturer not found: %s", lecturerIDStr))
		w.WriteHeader(http.StatusNotFound)
		return
	}

	sendUpdate(updateInfo{Resource: "lecturer", Operation: deletionOperation, Data: lecturerID})

	w.WriteHeader(http.StatusOK)
}

// Handler: GET /lecturer; Get all lecturers
func (s *Server) getLecturers(w http.ResponseWriter, r *http.Request) {
	lecturers, err := s.fetchLecturers(r.Context())

	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, lecturers)
}
