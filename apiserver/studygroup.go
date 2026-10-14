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

func registerStudyGroupRoutes(s *Server, mux *http.ServeMux) {
	mux.HandleFunc("GET /study-group/{id}", s.getStudyGroup)
	createHandler := requireContentType("application/json", true)(http.HandlerFunc(s.createStudyGroup))
	mux.Handle("POST /study-group", createHandler)
	mux.HandleFunc("DELETE /study-group/{id}", s.deleteStudyGroup)
	putHandler := requireContentType("application/json", true)(http.HandlerFunc(s.putStudyGroup))
	mux.Handle("PUT /study-group/{id}", putHandler)
	mux.HandleFunc("GET /study-group", s.getStudyGroups)
}

// StudyGroupBasicData contains the basic fields of a study group.
type StudyGroupBasicData struct {
	SemesterSeason *string          `json:"semester_season,omitempty"`
	SemesterYear   *int             `json:"semester_year,omitempty"`
	StudentCount   *int             `json:"student_count,omitempty"`
	DegreeProgram  *int             `json:"degree_program,omitempty"`
	ExtraData      *json.RawMessage `json:"extra_data,omitempty"`
}

// StudyGroup is the representation of a row in the 'study_group' database table.
type StudyGroup struct {
	ID int `json:"id"`
	StudyGroupBasicData
}

// CreateStudyGroupRequest is the representation of the expected JSON body for the POST /study-group endpoint.
type CreateStudyGroupRequest struct {
	Templates []int `json:"templates,omitempty"`
	StudyGroupBasicData
}

// PatchStudyGroupRequest is the representation of the expected JSON body for the PUT /study-group endpoint.
type PatchStudyGroupRequest CreateStudyGroupRequest

//go:embed queries/get_study_group.sql
var getStudyGroupQuery string

// Handler: GET /study-group/{id}; Get a single study group by ID
func (s *Server) getStudyGroup(w http.ResponseWriter, r *http.Request) {
	studyGroupIDStr := r.PathValue("id")
	studyGroupID, err := parseID(studyGroupIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	var studyGroup StudyGroup

	err = s.DB.QueryRow(ctx, getStudyGroupQuery, studyGroupID).Scan(
		&studyGroup.ID,
		&studyGroup.SemesterSeason,
		&studyGroup.SemesterYear,
		&studyGroup.StudentCount,
		&studyGroup.DegreeProgram,
		&studyGroup.ExtraData,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			logRequest(r, "Study group not found: "+studyGroupIDStr)
			w.WriteHeader(http.StatusNotFound)
		} else {
			logError(r, err)
			w.WriteHeader(http.StatusInternalServerError)
		}
		return
	}

	writeJSON(w, http.StatusOK, studyGroup)
}

//go:embed queries/create_study_group.sql
var createStudyGroupQuery string

//go:embed queries/add_courses_to_study_group_from_templates.sql
var addCoursesToStudyGroupFromTemplatesQuery string

// Handler: POST /study-group; Create a new study group
func (s *Server) createStudyGroup(w http.ResponseWriter, r *http.Request) {
	var request CreateStudyGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		logError(r, err)
		return
	}

	if request.SemesterSeason == nil || request.SemesterYear == nil || request.StudentCount == nil || request.DegreeProgram == nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Validate semester_season
	if *request.SemesterSeason != "winter" && *request.SemesterSeason != "summer" {
		logRequest(r, "Invalid semester_season: "+*request.SemesterSeason)
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

	tx, err := s.DB.Begin(ctx)

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		logError(r, err)
		return
	}

	defer tx.Rollback(ctx)

	var newStudyGroup StudyGroup
	err = tx.QueryRow(ctx, createStudyGroupQuery,
		request.SemesterSeason,
		request.SemesterYear,
		request.StudentCount,
		request.DegreeProgram,
		request.ExtraData,
	).Scan(
		&newStudyGroup.ID,
		&newStudyGroup.SemesterSeason,
		&newStudyGroup.SemesterYear,
		&newStudyGroup.StudentCount,
		&newStudyGroup.DegreeProgram,
		&newStudyGroup.ExtraData,
	)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			logRequest(r, fmt.Sprintf("%s violated; invalid reference", pgErr.ConstraintName))
			w.WriteHeader(http.StatusBadRequest)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
			logError(r, err)
		}
		return
	}

	var updates []updateInfo

	updates = append(updates, updateInfo{Resource: "studyGroup", Operation: createOperation, Data: newStudyGroup})

	newCourseStudyGroupRows, err := tx.Query(ctx, addCoursesToStudyGroupFromTemplatesQuery, newStudyGroup.ID, request.Templates)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		logError(r, err)
		return
	}

	for newCourseStudyGroupRows.Next() {
		var courseStudyGroup CourseStudyGroup
		err := newCourseStudyGroupRows.Scan(
			&courseStudyGroup.Course,
			&courseStudyGroup.StudyGroup,
		)

		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			logError(r, err)
			return
		}

		updates = append(updates, updateInfo{Resource: "courseStudyGroup", Operation: createOperation, Data: courseStudyGroup})
	}

	if err = newCourseStudyGroupRows.Err(); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		logError(r, err)
		return
	}

	if err = tx.Commit(ctx); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		logError(r, err)
		return
	}

	for _, update := range updates {
		sendUpdate(update)
	}

	writeJSON(w, http.StatusCreated, newStudyGroup)
}

//go:embed queries/delete_study_group.sql
var deleteStudyGroupQuery string

// Handler: DELETE /study-group/{id}; Delete a study group
func (s *Server) deleteStudyGroup(w http.ResponseWriter, r *http.Request) {
	studyGroupIDStr := r.PathValue("id")
	studyGroupID, err := parseID(studyGroupIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	result, err := s.DB.Exec(ctx, deleteStudyGroupQuery, studyGroupID)
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
		logRequest(r, "Study group not found: "+studyGroupIDStr)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	sendUpdate(updateInfo{Resource: "studyGroup", Operation: deletionOperation, Data: studyGroupID})

	w.WriteHeader(http.StatusOK)
}

//go:embed queries/update_study_group.sql
var updateStudyGroupQuery string

// Handler: PUT /study-group/{id}; Update an existing study group
func (s *Server) putStudyGroup(w http.ResponseWriter, r *http.Request) {
	studyGroupIDStr := r.PathValue("id")
	studyGroupID, err := parseID(studyGroupIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	var request PatchStudyGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Validate semester_season if provided
	if request.SemesterSeason != nil && *request.SemesterSeason != "winter" && *request.SemesterSeason != "summer" {
		logRequest(r, "Invalid semester_season: "+*request.SemesterSeason)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	var updatedStudyGroup StudyGroup
	err = s.DB.QueryRow(ctx, updateStudyGroupQuery,
		request.SemesterSeason,
		request.SemesterYear,
		request.StudentCount,
		request.DegreeProgram,
		request.ExtraData,
		studyGroupID,
	).Scan(
		&updatedStudyGroup.ID,
		&updatedStudyGroup.SemesterSeason,
		&updatedStudyGroup.SemesterYear,
		&updatedStudyGroup.StudentCount,
		&updatedStudyGroup.DegreeProgram,
		&updatedStudyGroup.ExtraData,
	)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			logRequest(r, fmt.Sprintf("%s violated; invalid reference", pgErr.ConstraintName))
			w.WriteHeader(http.StatusBadRequest)
		} else {
			logError(r, err)
			w.WriteHeader(http.StatusInternalServerError)
		}
		return
	}

	sendUpdate(updateInfo{Resource: "studyGroup", Operation: updateOperation, Data: updatedStudyGroup})

	writeJSON(w, http.StatusOK, updatedStudyGroup)
}

//go:embed queries/gg/study_groups.sql
var getStudyGroupsQuery string

func (s *Server) fetchStudyGroups(ctx context.Context) ([]StudyGroup, error) {
	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	rows, err := s.DB.Query(ctx, getStudyGroupsQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var studyGroups []StudyGroup
	for rows.Next() {
		var studyGroup StudyGroup
		err := rows.Scan(
			&studyGroup.ID,
			&studyGroup.SemesterSeason,
			&studyGroup.SemesterYear,
			&studyGroup.StudentCount,
			&studyGroup.DegreeProgram,
			&studyGroup.ExtraData,
		)
		if err != nil {
			return nil, err
		}
		studyGroups = append(studyGroups, studyGroup)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	// Return empty array instead of null if no study groups found
	if studyGroups == nil {
		studyGroups = []StudyGroup{}
	}

	return studyGroups, nil
}

// Handler: GET /study-group; Get all study groups
func (s *Server) getStudyGroups(w http.ResponseWriter, r *http.Request) {
	studyGroups, err := s.fetchStudyGroups(r.Context())

	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, studyGroups)
}
