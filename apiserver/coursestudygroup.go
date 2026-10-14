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

func registerCourseStudyGroupRoutes(s *Server, mux *http.ServeMux) {
	mux.HandleFunc("POST /course/{course_id}/study-group/{study_group_id}", s.createCourseStudyGroup)
	mux.HandleFunc("DELETE /course/{course_id}/study-group/{study_group_id}", s.deleteCourseStudyGroup)
	mux.HandleFunc("GET /course/{course_id}/study-groups", s.getCourseCourseStudyGroups)
	mux.HandleFunc("GET /course/study-groups", s.getCourseStudyGroups)
	// Prevent PUT/DELETE from matching /course/{id} wildcard routes
	mux.HandleFunc("PUT /course/study-groups", methodNotAllowed)
	mux.HandleFunc("DELETE /course/study-groups", methodNotAllowed)
}

//go:embed queries/create_course_study_group.sql
var createCourseStudyGroupQuery string

// Handler: POST /course/{course_id}/study-group/{study_group_id}; Assign a study group to a course
func (s *Server) createCourseStudyGroup(w http.ResponseWriter, r *http.Request) {
	courseIDStr := r.PathValue("course_id")
	courseID, err := parseID(courseIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	studyGroupIDStr := r.PathValue("study_group_id")
	studyGroupID, err := parseID(studyGroupIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	_, err = s.DB.Exec(ctx, createCourseStudyGroupQuery, courseID, studyGroupID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // Unique constraint violation
			logRequest(r, "Course-study-group assignment already exists")
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

	sendUpdate(updateInfo{Resource: "courseStudyGroup", Operation: createOperation, Data: CourseStudyGroup{Course: courseID, StudyGroup: studyGroupID}})

	w.WriteHeader(http.StatusCreated)
}

//go:embed queries/delete_course_study_group.sql
var deleteCourseStudyGroupQuery string

// Handler: DELETE /course/{course_id}/study-group/{study_group_id}; Remove a study group from a course
func (s *Server) deleteCourseStudyGroup(w http.ResponseWriter, r *http.Request) {
	courseIDStr := r.PathValue("course_id")
	courseID, err := parseID(courseIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	studyGroupIDStr := r.PathValue("study_group_id")
	studyGroupID, err := parseID(studyGroupIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	result, err := s.DB.Exec(ctx, deleteCourseStudyGroupQuery, courseID, studyGroupID)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if result.RowsAffected() == 0 {
		logRequest(r, "Course-study-group assignment not found")
		w.WriteHeader(http.StatusNotFound)
		return
	}

	sendUpdate(updateInfo{Resource: "courseStudyGroup", Operation: deletionOperation, Data: CourseStudyGroup{StudyGroup: studyGroupID, Course: courseID}})

	w.WriteHeader(http.StatusOK)
}

//go:embed queries/get_course_study_groups.sql
var getCourseCourseStudyGroupsQuery string

// Handler: GET /course/{course_id}/study-groups; Get all study groups assigned to a single course
func (s *Server) getCourseCourseStudyGroups(w http.ResponseWriter, r *http.Request) {
	courseIDStr := r.PathValue("course_id")
	courseID, err := parseID(courseIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	// one row all nulls if no results but exists; no rows if not exist; normal otherwise
	rows, err := s.DB.Query(ctx, getCourseCourseStudyGroupsQuery, courseID)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	if !rows.Next() {
		logRequest(r, "Course not found: "+courseIDStr)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	var emptyCheck *int
	// actual id never null
	err = rows.Scan(&emptyCheck, nil, nil, nil, nil)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if emptyCheck == nil {
		writeJSON(w, http.StatusOK, []StudyGroup{})
		return
	}

	var studyGroups []StudyGroup
	for {
		var studyGroup StudyGroup
		err := rows.Scan(
			&studyGroup.ID,
			&studyGroup.SemesterSeason,
			&studyGroup.SemesterYear,
			&studyGroup.StudentCount,
			&studyGroup.DegreeProgram,
		)
		if err != nil {
			logError(r, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		studyGroups = append(studyGroups, studyGroup)

		if !rows.Next() {
			break
		}
	}

	if err = rows.Err(); err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, studyGroups)
}

type CourseStudyGroup struct {
	Course     int `json:"course"`
	StudyGroup int `json:"study_group"`
}

//go:embed queries/gg/course_study_groups.sql
var getCourseStudyGroupsQuery string

func (s *Server) fetchCourseStudyGroups(ctx context.Context) ([]CourseStudyGroup, error) {
	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	rows, err := s.DB.Query(ctx, getCourseStudyGroupsQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var courseStudyGroups []CourseStudyGroup
	for rows.Next() {
		var courseStudyGroup CourseStudyGroup
		err := rows.Scan(
			&courseStudyGroup.Course,
			&courseStudyGroup.StudyGroup,
		)
		if err != nil {
			return nil, err
		}
		courseStudyGroups = append(courseStudyGroups, courseStudyGroup)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	// Return empty array instead of null if no course study groups found
	if courseStudyGroups == nil {
		courseStudyGroups = []CourseStudyGroup{}
	}

	return courseStudyGroups, nil
}

// Handler: GET /course/study-groups; Get all course-study-group assignments
func (s *Server) getCourseStudyGroups(w http.ResponseWriter, r *http.Request) {
	courseStudyGroups, err := s.fetchCourseStudyGroups(r.Context())

	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, courseStudyGroups)
}
