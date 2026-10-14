package apiserver

import "net/http"

// registerRoutes registers all routes for the API server.
// By convention, each API file contains a register[...](s *Server, mux *http.ServeMux) function,
// that registers individual routes.
func registerRoutes(s *Server) {
	mux := s.Mux

	api := http.NewServeMux()

	registerRoomRoutes(s, api)
	registerLecturerRoutes(s, api)
	registerCourseRoutes(s, api)
	registerTimeConstraintRoutes(s, api)
	registerDegreeProgramRoutes(s, api)
	registerStudyGroupRoutes(s, api)
	registerStudyGroupTemplateRoutes(s, api)
	registerStudyGroupTemplateCourseRoutes(s, api)
	registerCourseTimeSlotRoutes(s, api)
	registerCourseLecturerRoutes(s, api)
	registerCourseStudyGroupRoutes(s, api)
	registerCourseColorAssignmentRoutes(s, api)
	registerCourseColorRoutes(s, api)
	registerTimeSlotRoomAssignmentRoutes(s, api)
	registerGlobalGalaktischRoutes(s, api)
	registerImportExportRoutes(s, api)
	registerResourceConflictRoutes(s, api)
	registerLecturerTimeConstraintViolationRoutes(s, api)
	registerCSVRoutes(s, api)
	registerUpdateRoutes(s, api)

	// Individual register functions relative to /api/v1/
	mux.Handle("/api/v1/", http.StripPrefix("/api/v1", api))
}
