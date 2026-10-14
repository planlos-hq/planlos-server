package apiserver

import (
	"net/http"
)

func registerGlobalGalaktischRoutes(s *Server, mux *http.ServeMux) {
	mux.HandleFunc("GET /global-galaktisch", s.getGlobalGalaktisch)
}

type GlobalGalaktisch struct {
	Courses                   []Course                         `json:"courses"`
	CourseColors              []CourseColor                    `json:"course_colors"`
	CourseColorAssignments    []CourseColorAssignment          `json:"course_color_assignments"`
	CourseLecturers           []CourseLecturer                 `json:"course_lecturers"`
	CourseStudyGroups         []CourseStudyGroup               `json:"course_study_groups"`
	CourseTimeSlots           []CourseTimeSlot                 `json:"course_time_slots"`
	DegreePrograms            []DegreeProgram                  `json:"degree_programs"`
	Lecturers                 []Lecturer                       `json:"lecturers"`
	Rooms                     []Room                           `json:"rooms"`
	StudyGroups               []StudyGroup                     `json:"study_groups"`
	StudyGroupTemplates       []StudyGroupTemplate             `json:"study_group_templates"`
	StudyGroupTemplateCourses []StudyGroupTemplateCourse       `json:"study_group_template_courses"`
	TimeConstraints           []LecturerNegativeTimeConstraint `json:"time_constraints"`
	TimeSlotRoomAssignments   []TimeSlotRoomAssignment         `json:"time_slot_room_assignments"`
}

// Handler: GET /global-galaktisch; Get all resources at once for initial population of the frontend stores
func (s *Server) getGlobalGalaktisch(w http.ResponseWriter, r *http.Request) {
	var gg GlobalGalaktisch

	courses, err := s.fetchCourses(r.Context())

	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	gg.Courses = courses

	courseColors, err := s.fetchCourseColors(r.Context())

	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	gg.CourseColors = courseColors

	courseColorAssignments, err := s.fetchCourseColorAssignments(r.Context())

	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	gg.CourseColorAssignments = courseColorAssignments

	courseLecturers, err := s.fetchCourseLecturers(r.Context())

	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	gg.CourseLecturers = courseLecturers

	courseStudyGroups, err := s.fetchCourseStudyGroups(r.Context())

	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	gg.CourseStudyGroups = courseStudyGroups

	courseTimeSlots, err := s.fetchCourseTimeSlots(r.Context())

	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	gg.CourseTimeSlots = courseTimeSlots

	degreePrograms, err := s.fetchDegreePrograms(r.Context())

	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	gg.DegreePrograms = degreePrograms

	lecturers, err := s.fetchLecturers(r.Context())

	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	gg.Lecturers = lecturers

	rooms, err := s.fetchRooms(r.Context())

	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	gg.Rooms = rooms

	studyGroups, err := s.fetchStudyGroups(r.Context())

	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	gg.StudyGroups = studyGroups

	studyGroupTemplates, err := s.fetchStudyGroupTemplates(r.Context())

	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	gg.StudyGroupTemplates = studyGroupTemplates

	studyGroupTemplateCourses, err := s.fetchStudyGroupTemplateCourses(r.Context())

	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	gg.StudyGroupTemplateCourses = studyGroupTemplateCourses

	timeConstraints, err := s.fetchLecturerNegativeTimeConstraints(r.Context())

	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	gg.TimeConstraints = timeConstraints

	timeSlotRoomAssignments, err := s.fetchTimeSlotRoomAssignments(r.Context())

	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	gg.TimeSlotRoomAssignments = timeSlotRoomAssignments

	writeJSON(w, http.StatusOK, gg)
}
