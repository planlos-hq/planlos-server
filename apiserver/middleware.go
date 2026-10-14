package apiserver

import (
	"bytes"
	"context"
	_ "embed"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Currently not needed due to docker deployment with reverse proxy
var allowedOrigins = map[string]bool{ //nolint:gochecknoglobals
	"http://localhost": true,
}

// corsMiddleware sets the correct CORS Headers and handles OPTIONS (preflight) requests.
// Ref. internal docs
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		if allowedOrigins[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}

		// Allow Cookies to be sent cross-origin
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		// Handle preflight, allow everything for now
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// requireContentType rejects requests that don't have the expected content type with status 415.
// Also checks for null bytes in the body and rejects them with status 400.
func requireContentType(expected string, allowEmpty bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			contentType := r.Header.Get("Content-Type")
			if !strings.HasPrefix(contentType, expected) || (contentType == "" && !allowEmpty) {
				w.WriteHeader(http.StatusUnsupportedMediaType)
				return
			}

			// Check for null bytes in JSON body (These would otherwise result in 500).
			// This is a potential performance issue (one copy, three iterations of the request body),
			// but we don't deal with large data volumes anyways.
			body, err := io.ReadAll(r.Body) // destructive
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			// Catch both "real" null byte and escaped version
			if bytes.ContainsRune(body, '\x00') || bytes.Contains(body, []byte(`\u0000`)) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			// Re-set request body
			r.Body = io.NopCloser(bytes.NewReader(body))

			next.ServeHTTP(w, r)
		})
	}
}

// Maintenance gate
var (
	maintenance = atomic.Bool{}  // Stop new DB connections
	dbMu        = sync.RWMutex{} // DB mutex
)

// maintenanceGate blocks all requests / database connections during operations that require an exclusive lock
func maintenanceGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Import / restore / truncate requests bypass the gate, they take the write lock themselves
		isMaintenanceRequest := r.Method == http.MethodPost &&
			(r.URL.Path == "/api/v1/import/dump" ||
				r.URL.Path == "/api/v1/truncate" ||
				strings.HasPrefix(r.URL.Path, "/api/v1/backups/restore/"))
		// The SSE stream stays open while the app is open and never touches the DB;
		// holding the read lock would block maintenance requests forever
		isUpdateStream := r.Method == http.MethodGet && r.URL.Path == "/api/v1/updates"
		if isUpdateStream {
			next.ServeHTTP(w, r)
		} else if !isMaintenanceRequest {
			// Deny new requests during maintenance
			if maintenance.Load() {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			// Regular requests take a read lock (must finish before X-locking ops)
			dbMu.RLock()
			defer dbMu.RUnlock()
			next.ServeHTTP(w, r)
		} else {
			next.ServeHTTP(w, r)
		}
	})
}

// methodNotAllowed is a handler that always returns 405.
// This is used to prevent false matching of routes like PUT /lecturer/negative-time-constraint as PUT /lecturer/{id}
func methodNotAllowed(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusMethodNotAllowed)
}

// statusRecorder saves the http status code for logging purposes
type statusRecorder struct {
	http.ResponseWriter

	status int
}

// WriteHeader writes the http status in the response header and saves it in the statusRecorder for logging
func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// as required by [http.NewResponseController()]
func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// loggingMiddleware logs Method, Path, HTTP status and elapsed time for each request
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rec, r)

		log.Printf("%s %s %d %v", r.Method, r.URL.Path, rec.status, time.Since(start))
	})
}

type ForbiddenResponse struct {
	ShibUsername string `json:"shib_username"`
}

//go:embed queries/is_user_authorized.sql
var isUserAuthorizedQuery string

// authorizedUsersMiddleware validates the Shibboleth username from the Remote_user header against the db
func (s *Server) authorizedUsersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const timeout = 3 * time.Second
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()

		var isAuthorized bool

		shibUser := r.Header.Get("Remote_user")

		err := s.DB.QueryRow(ctx, isUserAuthorizedQuery, shibUser).Scan(&isAuthorized)

		if err != nil {
			logError(r, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		if !isAuthorized {
			writeJSON(w, http.StatusForbidden, ForbiddenResponse{ShibUsername: shibUser}) // not http.StatusUnauthorized since that indicates that user is not logged in which should not be possible
			return
		}

		next.ServeHTTP(w, r)
	})
}
