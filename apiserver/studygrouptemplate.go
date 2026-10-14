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

func registerStudyGroupTemplateRoutes(s *Server, mux *http.ServeMux) {
	mux.HandleFunc("GET /study-group-template/{id}", s.getStudyGroupTemplate)
	createHandler := requireContentType("application/json", true)(http.HandlerFunc(s.createStudyGroupTemplate))
	mux.Handle("POST /study-group-template", createHandler)
	mux.HandleFunc("DELETE /study-group-template/{id}", s.deleteStudyGroupTemplate)
	putHandler := requireContentType("application/json", true)(http.HandlerFunc(s.putStudyGroupTemplate))
	mux.Handle("PUT /study-group-template/{id}", putHandler)
	mux.HandleFunc("GET /study-group-template", s.getStudyGroupTemplates)
}

type StudyGroupTemplateBasicData struct {
	Name *string `json:"name,omitempty"`
}

// StudyGroupTemplate is the representation of a row in the 'study_group_template' database table.
type StudyGroupTemplate struct {
	ID int `json:"id"`
	StudyGroupTemplateBasicData
}

// CreateStudyGroupTemplateRequest is the representation of the expected JSON body for the POST /study-group-template endpoint.
type CreateStudyGroupTemplateRequest StudyGroupTemplateBasicData

// PatchStudyGroupTemplateRequest is the representation of the expected JSON body for the PUT /study-group-template endpoint.
type PatchStudyGroupTemplateRequest CreateStudyGroupTemplateRequest

//go:embed queries/get_study_group_template.sql
var getStudyGroupTemplateQuery string

// Handler: GET /study-group-template/{id}; Get a single study group template by ID
func (s *Server) getStudyGroupTemplate(w http.ResponseWriter, r *http.Request) {
	templateIDStr := r.PathValue("id")
	templateID, err := parseID(templateIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	var template StudyGroupTemplate

	err = s.DB.QueryRow(ctx, getStudyGroupTemplateQuery, templateID).Scan(&template.ID, &template.Name)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			logRequest(r, "Course not found: "+templateIDStr)
			w.WriteHeader(http.StatusNotFound)
		} else {
			logError(r, err)
			w.WriteHeader(http.StatusInternalServerError)
		}
		return
	}

	writeJSON(w, http.StatusOK, template)
}

//go:embed queries/create_study_group_template.sql
var createStudyGroupTemplateQuery string

// Handler: POST /study-group-template; Create a new study group template
func (s *Server) createStudyGroupTemplate(w http.ResponseWriter, r *http.Request) {
	var request CreateStudyGroupTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		logError(r, err)
		return
	}

	if request.Name == nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	var newTemplate StudyGroupTemplate
	err := s.DB.QueryRow(ctx, createStudyGroupTemplateQuery, request.Name).Scan(&newTemplate.ID, &newTemplate.Name)

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		logError(r, err)
		return
	}

	sendUpdate(updateInfo{Resource: "studyGroupTemplate", Operation: createOperation, Data: newTemplate})

	writeJSON(w, http.StatusCreated, newTemplate)
}

//go:embed queries/delete_study_group_template.sql
var deleteStudyGroupTemplateQuery string

// Handler: DELETE /study-group-template/{id}; Delete a study group template
func (s *Server) deleteStudyGroupTemplate(w http.ResponseWriter, r *http.Request) {
	templateIDStr := r.PathValue("id")
	templateID, err := parseID(templateIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	result, err := s.DB.Exec(ctx, deleteStudyGroupTemplateQuery, templateID)
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
		logRequest(r, "Study-Group-Template not found: "+templateIDStr)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	sendUpdate(updateInfo{Resource: "studyGroupTemplate", Operation: deletionOperation, Data: templateID})

	w.WriteHeader(http.StatusOK)
}

//go:embed queries/update_study_group_template.sql
var updateStudyGroupTemplateQuery string

// Handler: PUT /study-group-template/{id}; Update an existing study group template
func (s *Server) putStudyGroupTemplate(w http.ResponseWriter, r *http.Request) {
	templateIDStr := r.PathValue("id")
	templateID, err := parseID(templateIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	var request PatchStudyGroupTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	var updatedTemplate StudyGroupTemplate
	err = s.DB.QueryRow(ctx, updateStudyGroupTemplateQuery, request.Name, templateID).Scan(&updatedTemplate.ID, &updatedTemplate.Name)

	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	sendUpdate(updateInfo{Resource: "studyGroupTemplate", Operation: updateOperation, Data: updatedTemplate})

	writeJSON(w, http.StatusOK, updatedTemplate)
}

//go:embed queries/gg/study_group_templates.sql
var getStudyGroupTemplatesQuery string

func (s *Server) fetchStudyGroupTemplates(ctx context.Context) ([]StudyGroupTemplate, error) {
	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	rows, err := s.DB.Query(ctx, getStudyGroupTemplatesQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var templates []StudyGroupTemplate
	for rows.Next() {
		var template StudyGroupTemplate
		err := rows.Scan(
			&template.ID,
			&template.Name,
		)
		if err != nil {
			return nil, err
		}
		templates = append(templates, template)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	// Return empty array instead of null if no courses found
	if templates == nil {
		templates = []StudyGroupTemplate{}
	}

	return templates, nil
}

// Handler: GET /study-group-template; Get all study group templates
func (s *Server) getStudyGroupTemplates(w http.ResponseWriter, r *http.Request) {
	templates, err := s.fetchStudyGroupTemplates(r.Context())
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, templates)
}
