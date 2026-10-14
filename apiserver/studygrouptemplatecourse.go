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

func registerStudyGroupTemplateCourseRoutes(s *Server, mux *http.ServeMux) {
	mux.HandleFunc("POST /study-group-template/{template_id}/course/{course_id}", s.createStudyGroupTemplateCourse)
	mux.HandleFunc("DELETE /study-group-template/{template_id}/course/{course_id}", s.deleteStudyGroupTemplateCourse)
	mux.HandleFunc("GET /study-group-template/{template_id}/courses", s.getStudyGroupTemplateStudyGroupTemplateCourses)
	mux.HandleFunc("GET /study-group-template/courses", s.getStudyGroupTemplateCourses)
	// Prevent PUT/DELETE from matching /study-group-template/{id} wildcard routes
	mux.HandleFunc("PUT /study-group-template/courses", methodNotAllowed)
	mux.HandleFunc("DELETE /study-group-template/courses", methodNotAllowed)
}

//go:embed queries/create_study_group_template_course.sql
var createStudyGroupTemplateCourseQuery string

// Handler: POST /study-group-template/{template_id}/course/{course_id}; Assign a course to a study group template
func (s *Server) createStudyGroupTemplateCourse(w http.ResponseWriter, r *http.Request) {
	templateIDStr := r.PathValue("template_id")
	templateID, err := parseID(templateIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

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

	_, err = s.DB.Exec(ctx, createStudyGroupTemplateCourseQuery, templateID, courseID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // Unique constraint violation
			logRequest(r, "StudyGroupTemplate-Course assignment already exists")
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

	sendUpdate(updateInfo{Resource: "studyGroupTemplateCourse", Operation: createOperation, Data: StudyGroupTemplateCourse{Template: templateID, Course: courseID}})

	w.WriteHeader(http.StatusCreated)
}

//go:embed queries/delete_study_group_template_course.sql
var deleteStudyGroupTemplateCourseQuery string

// Handler: DELETE /study-group-template/{template_id}/course/{course_id}; Remove a course from a study group template
func (s *Server) deleteStudyGroupTemplateCourse(w http.ResponseWriter, r *http.Request) {
	templateIDStr := r.PathValue("template_id")
	templateID, err := parseID(templateIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

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

	result, err := s.DB.Exec(ctx, deleteStudyGroupTemplateCourseQuery, templateID, courseID)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if result.RowsAffected() == 0 {
		logRequest(r, "StudyGroupTemplate-Course assignment not found")
		w.WriteHeader(http.StatusNotFound)
		return
	}

	sendUpdate(updateInfo{Resource: "studyGroupTemplateCourse", Operation: deletionOperation, Data: StudyGroupTemplateCourse{Template: templateID, Course: courseID}})

	w.WriteHeader(http.StatusOK)
}

//go:embed queries/get_study_group_template_study_group_template_courses.sql
var getStudyGroupTemplateStudyGroupTemplateCoursesQuery string

// Handler: GET /study-group-template/{template_id}/courses; Get all courses assigned to a single study group template
func (s *Server) getStudyGroupTemplateStudyGroupTemplateCourses(w http.ResponseWriter, r *http.Request) {
	templateIDStr := r.PathValue("template_id")
	templateID, err := parseID(templateIDStr)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	// one row all nulls if no results but exists; no rows if not exist; normal otherwise
	rows, err := s.DB.Query(ctx, getStudyGroupTemplateStudyGroupTemplateCoursesQuery, templateID)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	if !rows.Next() {
		logRequest(r, "StudyGroupTemplate not found: "+templateIDStr)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	var emptyCheck *int
	// actual id never null
	err = rows.Scan(&emptyCheck, nil, nil, nil, nil, nil, nil, nil)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if emptyCheck == nil {
		writeJSON(w, http.StatusOK, []Course{})
		return
	}

	var courses []Course
	for {
		var course Course
		err := rows.Scan(
			&course.ID,
			&course.Number,
			&course.Name,
			&course.SWS,
			&course.CreditPoints,
			&course.Abbreviation,
			&course.ExtraData,
		)
		if err != nil {
			logError(r, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		courses = append(courses, course)

		if !rows.Next() {
			break
		}
	}

	if err = rows.Err(); err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, courses)
}

type StudyGroupTemplateCourse struct {
	Template int `json:"template"`
	Course   int `json:"course"`
}

//go:embed queries/gg/study_group_template_courses.sql
var getStudyGroupTemplateCoursesQuery string

func (s *Server) fetchStudyGroupTemplateCourses(ctx context.Context) ([]StudyGroupTemplateCourse, error) {
	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	rows, err := s.DB.Query(ctx, getStudyGroupTemplateCoursesQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var templateCourses []StudyGroupTemplateCourse
	for rows.Next() {
		var templateCourse StudyGroupTemplateCourse
		err := rows.Scan(
			&templateCourse.Template,
			&templateCourse.Course,
		)
		if err != nil {
			return nil, err
		}
		templateCourses = append(templateCourses, templateCourse)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	// Return empty array instead of null if no study group template courses found
	if templateCourses == nil {
		templateCourses = []StudyGroupTemplateCourse{}
	}

	return templateCourses, nil
}

// Handler: GET /study-group-template/courses; Get all study-group-template-course assignments
func (s *Server) getStudyGroupTemplateCourses(w http.ResponseWriter, r *http.Request) {
	templateCourses, err := s.fetchStudyGroupTemplateCourses(r.Context())

	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, templateCourses)
}
