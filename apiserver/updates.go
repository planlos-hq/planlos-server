package apiserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

func registerUpdateRoutes(s *Server, mux *http.ServeMux) {
	mux.HandleFunc("GET /updates", s.updates)
}

type operation string

const (
	createOperation     operation = "create"
	updateOperation     operation = "update"
	deletionOperation   operation = "delete"
	dumpImportOperation operation = "dumpImport"
)

// updateInfo represents a SSE update message
type updateInfo struct {
	Resource  string    `json:"resource"`
	Operation operation `json:"operation"`
	Data      any       `json:"data"`
}

var updateChans []chan updateInfo = make([]chan updateInfo, 0)
var updateChansMutex sync.RWMutex

// sendUpdate sends an update (SSE) to all registered clients
func sendUpdate(update updateInfo) {
	updateChansMutex.RLock()
	for _, c := range updateChans {
		select {
		case c <- update:
		default:
		}

	}
	updateChansMutex.RUnlock()
}

// updates establishes the SSE connection for live-updates.
// Sends a ping every 30 seconds and the updates (updateInfo) sent via sendUpdate.
// Handler: GET /updates; Server-Sent-Events stream that pushes live updates to the client
func (s *Server) updates(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	rc.SetWriteDeadline(time.Time{})

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	rc.Flush()

	c := make(chan updateInfo, 100)

	updateChansMutex.Lock()
	updateChans = append(updateChans, c)
	updateChansMutex.Unlock()

	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()

outer:
	for {
		select {
		case <-r.Context().Done():
			break outer
		case <-pingTicker.C:
			_, err := fmt.Fprintf(w, ": ping\n\n")
			if err != nil {
				logError(r, err)
				break outer
			}
			rc.Flush()
		case update := <-c:
			data, err := json.Marshal(update)
			if err != nil {
				logError(r, err)
				break outer
			}
			_, err = fmt.Fprintf(w, "data: %s\n\n", string(data))
			if err != nil {
				logError(r, err)
				break outer
			}
			rc.Flush()
		}
	}

	updateChansMutex.Lock()
	for i, ch := range updateChans {
		if ch == c {
			updateChans[i] = updateChans[len(updateChans)-1]
			updateChans = updateChans[:len(updateChans)-1]
			break
		}
	}
	updateChansMutex.Unlock()
}
