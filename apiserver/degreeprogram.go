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

func registerDegreeProgramRoutes(s *Server, mux *http.ServeMux) {
	mux.HandleFunc("GET /degree-program/{id}", s.getDegreeProgram)
	createHandler := requireContentType("application/json", true)(http.HandlerFunc(s.createDegreeProgram))
	mux.Handle("POST /degree-program", createHandler)
	mux.HandleFunc("DELETE /degree-program/{id}", s.deleteDegreeProgram)
	putHandler := requireContentType("application/json", true)(http.HandlerFunc(s.putDegreeProgram))
	mux.Handle("PUT /degree-program/{id}", putHandler)
	mux.HandleFunc("GET /degree-program", s.getDegreePrograms)
}

// DegreeProgramBasicData contains the basic fields of a degree program.
type DegreeProgramBasicData struct {
	Name         *string          `json:"name,omitempty"`
	Abbreviation *string          `json:"abbreviation,omitempty"`
	ExtraData    *json.RawMessage `json:"extra_data,omitempty"`
}

// DegreeProgram is the representation of a row in the 'degree_program' database table.
type DegreeProgram struct {
	ID int `json:"id"`
	DegreeProgramBasicData
}

// CreateDegreeProgramRequest is the representation of the expected JSON body for the POST /degree-program endpoint.
type CreateDegreeProgramRequest DegreeProgramBasicData

// PatchDegreeProgramRequest is the representation of the expected JSON body for the PUT /degree-program endpoint.
type PatchDegreeProgramRequest CreateDegreeProgramRequest

//go:embed queries/get_degree_program.sql
var getDegreeProgramQuery string

// Handler: GET /degree-program/{id}; Get a single degree program by ID
func (s *Server) getDegreeProgram(w http.ResponseWriter, r *http.Request) {
	degreeProgramIDStr := r.PathValue("id")
	degreeProgramID, err := parseID(degreeProgramIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	var degreeProgram DegreeProgram

	err = s.DB.QueryRow(ctx, getDegreeProgramQuery, degreeProgramID).Scan(
		&degreeProgram.ID,
		&degreeProgram.Name,
		&degreeProgram.Abbreviation,
		&degreeProgram.ExtraData,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			logRequest(r, "Degree program not found: "+degreeProgramIDStr)
			w.WriteHeader(http.StatusNotFound)
		} else {
			logError(r, err)
			w.WriteHeader(http.StatusInternalServerError)
		}
		return
	}

	writeJSON(w, http.StatusOK, degreeProgram)
}

//go:embed queries/create_degree_program.sql
var createDegreeProgramQuery string

// Handler: POST /degree-program; Create a new degree program
func (s *Server) createDegreeProgram(w http.ResponseWriter, r *http.Request) {
	var request CreateDegreeProgramRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		logError(r, err)
		return
	}

	if request.Name == nil || request.Abbreviation == nil {
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

	var newDegreeProgram DegreeProgram
	err := s.DB.QueryRow(ctx, createDegreeProgramQuery,
		request.Name,
		request.Abbreviation,
		request.ExtraData,
	).Scan(
		&newDegreeProgram.ID,
		&newDegreeProgram.Name,
		&newDegreeProgram.Abbreviation,
		&newDegreeProgram.ExtraData,
	)

	if err != nil {
		// Check if it's a unique constraint violation
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			logRequest(r, "Degree program name already exists: "+*request.Name)
			w.WriteHeader(http.StatusConflict)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
			logError(r, err)
		}
		return
	}

	sendUpdate(updateInfo{Resource: "degreeProgram", Operation: createOperation, Data: newDegreeProgram})

	writeJSON(w, http.StatusCreated, newDegreeProgram)
}

//go:embed queries/delete_degree_program.sql
var deleteDegreeProgramQuery string

// Handler: DELETE /degree-program/{id}; Delete a degree program
func (s *Server) deleteDegreeProgram(w http.ResponseWriter, r *http.Request) {
	degreeProgramIDStr := r.PathValue("id")
	degreeProgramID, err := parseID(degreeProgramIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	result, err := s.DB.Exec(ctx, deleteDegreeProgramQuery, degreeProgramID)
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
		logRequest(r, "Degree program not found: "+degreeProgramIDStr)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	sendUpdate(updateInfo{Resource: "degreeProgram", Operation: deletionOperation, Data: degreeProgramID})

	w.WriteHeader(http.StatusOK)
}

//go:embed queries/update_degree_program.sql
var updateDegreeProgramQuery string

// Handler: PUT /degree-program/{id}; Update an existing degree program
func (s *Server) putDegreeProgram(w http.ResponseWriter, r *http.Request) {
	degreeProgramIDStr := r.PathValue("id")
	degreeProgramID, err := parseID(degreeProgramIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	var request PatchDegreeProgramRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	var updatedDegreeProgram DegreeProgram
	err = s.DB.QueryRow(ctx, updateDegreeProgramQuery,
		request.Name,
		request.Abbreviation,
		request.ExtraData,
		degreeProgramID,
	).Scan(
		&updatedDegreeProgram.ID,
		&updatedDegreeProgram.Name,
		&updatedDegreeProgram.Abbreviation,
		&updatedDegreeProgram.ExtraData,
	)

	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	sendUpdate(updateInfo{Resource: "degreeProgram", Operation: updateOperation, Data: updatedDegreeProgram})

	writeJSON(w, http.StatusOK, updatedDegreeProgram)
}

//go:embed queries/gg/degree_programs.sql
var getDegreeProgramsQuery string

func (s *Server) fetchDegreePrograms(ctx context.Context) ([]DegreeProgram, error) {
	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	rows, err := s.DB.Query(ctx, getDegreeProgramsQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var degreePrograms []DegreeProgram
	for rows.Next() {
		var degreeProgram DegreeProgram
		err := rows.Scan(
			&degreeProgram.ID,
			&degreeProgram.Name,
			&degreeProgram.Abbreviation,
			&degreeProgram.ExtraData,
		)
		if err != nil {
			return nil, err
		}
		degreePrograms = append(degreePrograms, degreeProgram)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	// Return empty array instead of null if no degree programs found
	if degreePrograms == nil {
		degreePrograms = []DegreeProgram{}
	}

	return degreePrograms, nil
}

// Handler: GET /degree-program; Get all degree programs
func (s *Server) getDegreePrograms(w http.ResponseWriter, r *http.Request) {
	degreePrograms, err := s.fetchDegreePrograms(r.Context())

	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, degreePrograms)
}
