package apiserver

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

func registerTimeConstraintRoutes(s *Server, mux *http.ServeMux) {
	mux.HandleFunc("GET /lecturer-negative-time-constraint/{id}", s.getLecturerNegativeTimeConstraint)
	createHandler := requireContentType("application/json", true)(http.HandlerFunc(s.createLecturerNegativeTimeConstraint))
	mux.Handle("POST /lecturer-negative-time-constraint", createHandler)
	mux.HandleFunc("DELETE /lecturer-negative-time-constraint/{id}", s.deleteLecturerNegativeTimeConstraint)
	putHandler := requireContentType("application/json", true)(http.HandlerFunc(s.putLecturerNegativeTimeConstraint))
	mux.Handle("PUT /lecturer-negative-time-constraint/{id}", putHandler)
	mux.HandleFunc("GET /lecturer/{id}/negative-time-constraint", s.getLecturerLecturerNegativeTimeConstraints)
	mux.HandleFunc("GET /lecturer/negative-time-constraint", s.getLecturerNegativeTimeConstraints)
	// Prevent PUT from matching PUT /lecturer/{id} wildcard
	mux.HandleFunc("PUT /lecturer/negative-time-constraint", methodNotAllowed)
}

type LecturerNegativeTimeConstraintBasicData struct {
	Lecturer *int    `json:"lecturer,omitempty"`
	Weekday  *int    `json:"weekday,omitempty"`
	At       *string `json:"at,omitempty"`
	Duration *string `json:"duration,omitempty"`
	Soft     *bool   `json:"soft,omitempty"`
}

// LecturerNegativeTimeConstraint is the representation of a row in the 'lecturer_negative_time_constraint' database table.
type LecturerNegativeTimeConstraint struct {
	ID int `json:"id"`
	LecturerNegativeTimeConstraintBasicData
}

// CreateLecturerNegativeTimeConstraintRequest is the representation of the expected JSON body for the POST /lecturer-negative-time-constraint endpoint.
type CreateLecturerNegativeTimeConstraintRequest LecturerNegativeTimeConstraintBasicData

// PatchLecturerNegativeTimeConstraintRequest is the representation of the expected JSON body for the PUT /lecturer-negative-time-constraint endpoint.
type PatchLecturerNegativeTimeConstraintRequest CreateLecturerNegativeTimeConstraintRequest

//go:embed queries/get_lecturer_negative_time_constraint.sql
var getLecturerNegativeTimeConstraintQuery string

// Handler: GET /lecturer-negative-time-constraint/{id}; Get a single lecturer negative time constraint by ID
func (s *Server) getLecturerNegativeTimeConstraint(w http.ResponseWriter, r *http.Request) {
	constraintIDStr := r.PathValue("id")
	constraintID, err := parseID(constraintIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	var constraint LecturerNegativeTimeConstraint

	err = s.DB.QueryRow(ctx, getLecturerNegativeTimeConstraintQuery, constraintID).Scan(
		&constraint.ID,
		&constraint.Lecturer,
		&constraint.Weekday,
		&constraint.At,
		&constraint.Duration,
		&constraint.Soft,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			logRequest(r, "Lecturer negative time constraint not found: "+constraintIDStr)
			w.WriteHeader(http.StatusNotFound)
		} else {
			logError(r, err)
			w.WriteHeader(http.StatusInternalServerError)
		}
		return
	}

	writeJSON(w, http.StatusOK, constraint)
}

//go:embed queries/create_lecturer_negative_time_constraint.sql
var createLecturerNegativeTimeConstraintQuery string

// Handler: POST /lecturer-negative-time-constraint; Create a new lecturer negative time constraint
func (s *Server) createLecturerNegativeTimeConstraint(w http.ResponseWriter, r *http.Request) {
	var request CreateLecturerNegativeTimeConstraintRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		logError(r, err)
		return
	}

	if request.Lecturer == nil || request.Weekday == nil || request.At == nil || request.Duration == nil || request.Soft == nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	var newConstraint LecturerNegativeTimeConstraint
	err := s.DB.QueryRow(ctx, createLecturerNegativeTimeConstraintQuery,
		request.Lecturer,
		request.Weekday,
		request.At,
		request.Duration,
		request.Soft,
	).Scan(
		&newConstraint.ID,
		&newConstraint.Lecturer,
		&newConstraint.Weekday,
		&newConstraint.At,
		&newConstraint.Duration,
		&newConstraint.Soft,
	)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			logRequest(r, fmt.Sprintf("%s violated; invalid reference", pgErr.ConstraintName))
			w.WriteHeader(http.StatusBadRequest)
		} else if pgErr != nil && pgErr.Code == "23514" {
			w.WriteHeader(http.StatusBadRequest)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
		logError(r, err)
		return
	}

	sendUpdate(updateInfo{Resource: "lecturerNegativeTimeConstraint", Operation: createOperation, Data: newConstraint})

	writeJSON(w, http.StatusCreated, newConstraint)
}

//go:embed queries/delete_lecturer_negative_time_constraint.sql
var deleteLecturerNegativeTimeConstraintQuery string

// Handler: DELETE /lecturer-negative-time-constraint/{id}; Delete a lecturer negative time constraint
func (s *Server) deleteLecturerNegativeTimeConstraint(w http.ResponseWriter, r *http.Request) {
	constraintIDStr := r.PathValue("id")
	constraintID, err := parseID(constraintIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	result, err := s.DB.Exec(ctx, deleteLecturerNegativeTimeConstraintQuery, constraintID)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if result.RowsAffected() == 0 {
		logRequest(r, "Lecturer negative time constraint not found: "+constraintIDStr)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	sendUpdate(updateInfo{Resource: "lecturerNegativeTimeConstraint", Operation: deletionOperation, Data: constraintID})

	w.WriteHeader(http.StatusOK)
}

//go:embed queries/update_lecturer_negative_time_constraint.sql
var updateLecturerNegativeTimeConstraintQuery string

// Handler: PUT /lecturer-negative-time-constraint/{id}; Update an existing lecturer negative time constraint
func (s *Server) putLecturerNegativeTimeConstraint(w http.ResponseWriter, r *http.Request) {
	constraintIDStr := r.PathValue("id")
	constraintID, err := parseID(constraintIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	var request PatchLecturerNegativeTimeConstraintRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	var updatedConstraint LecturerNegativeTimeConstraint
	err = s.DB.QueryRow(ctx, updateLecturerNegativeTimeConstraintQuery,
		request.Lecturer,
		request.Weekday,
		request.At,
		request.Duration,
		request.Soft,
		constraintID,
	).Scan(
		&updatedConstraint.ID,
		&updatedConstraint.Lecturer,
		&updatedConstraint.Weekday,
		&updatedConstraint.At,
		&updatedConstraint.Duration,
		&updatedConstraint.Soft,
	)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			logRequest(r, fmt.Sprintf("%s violated; invalid reference", pgErr.ConstraintName))
			w.WriteHeader(http.StatusBadRequest)
		} else if pgErr != nil && pgErr.Code == "23514" {
			w.WriteHeader(http.StatusBadRequest)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
		logError(r, err)
		return
	}

	sendUpdate(updateInfo{Resource: "lecturerNegativeTimeConstraint", Operation: updateOperation, Data: updatedConstraint})

	writeJSON(w, http.StatusOK, updatedConstraint)
}

//go:embed queries/get_lecturer_negative_time_constraints.sql
var getLecturerLecturerNegativeTimeConstraintsQuery string

// Handler: GET /lecturer/{id}/negative-time-constraint; Get all negative time constraints of a single lecturer
func (s *Server) getLecturerLecturerNegativeTimeConstraints(w http.ResponseWriter, r *http.Request) {
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

	rows, err := s.DB.Query(ctx, getLecturerLecturerNegativeTimeConstraintsQuery, lecturerID)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var constraints []LecturerNegativeTimeConstraint
	for rows.Next() {
		var constraint LecturerNegativeTimeConstraint
		err := rows.Scan(
			&constraint.ID,
			&constraint.Lecturer,
			&constraint.Weekday,
			&constraint.At,
			&constraint.Duration,
			&constraint.Soft,
		)
		if err != nil {
			logError(r, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		constraints = append(constraints, constraint)
	}

	if err = rows.Err(); err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Return empty array instead of null if no constraints found
	if constraints == nil {
		constraints = []LecturerNegativeTimeConstraint{}
	}

	writeJSON(w, http.StatusOK, constraints)
}

//go:embed queries/gg/lecturer_negative_time_constraints.sql
var getLecturerNegativeTimeConstraintsQuery string

func (s *Server) fetchLecturerNegativeTimeConstraints(ctx context.Context) ([]LecturerNegativeTimeConstraint, error) {
	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	rows, err := s.DB.Query(ctx, getLecturerNegativeTimeConstraintsQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var constraints []LecturerNegativeTimeConstraint
	for rows.Next() {
		var constraint LecturerNegativeTimeConstraint
		err := rows.Scan(
			&constraint.ID,
			&constraint.Lecturer,
			&constraint.Weekday,
			&constraint.At,
			&constraint.Duration,
			&constraint.Soft,
		)
		if err != nil {
			return nil, err
		}
		constraints = append(constraints, constraint)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	// Return empty array instead of null if no constraints found
	if constraints == nil {
		constraints = []LecturerNegativeTimeConstraint{}
	}

	return constraints, nil
}

// Handler: GET /lecturer/negative-time-constraint; Get all lecturer negative time constraints
func (s *Server) getLecturerNegativeTimeConstraints(w http.ResponseWriter, r *http.Request) {
	constraints, err := s.fetchLecturerNegativeTimeConstraints(r.Context())

	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, constraints)
}
