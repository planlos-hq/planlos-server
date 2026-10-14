// Package apiserver provides the Server struct, which contains the API state.
// It also contains the necessary middleware, routes and endpoints to operate the API.
package apiserver

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The Server represents the API state, including the database connection and ServeMux.
type Server struct {
	DB   *pgxpool.Pool
	Mux  *http.ServeMux
	root http.Handler
}

// New returns a Server object with db and a new ServeMux.
// It also wraps the root handler in middleware and registers all API routes.
func New(db *pgxpool.Pool, noAuth bool) *Server {
	server := &Server{
		DB:  db,
		Mux: http.NewServeMux(),
	}
	registerRoutes(server)

	// Wrap with global middleware
	var h http.Handler = server.Mux
	h = corsMiddleware(h)
	if !noAuth {
		h = server.authorizedUsersMiddleware(h)
	}
	h = loggingMiddleware(h)
	h = maintenanceGate(h)
	server.root = h

	return server
}

// Handler returns the root http handler from s.
func (s *Server) Handler() http.Handler {
	return s.root
}
