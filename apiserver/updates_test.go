package apiserver

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestUpdatesEndpoint(t *testing.T) {
	pool := setupTestDB(t)
	defer pool.Close()

	server := New(pool, true) // noAuth = true
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	// Use a recorder for more control over the stream
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/updates", nil)
	
	client := http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to connect to updates: %v", err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("Expected Content-Type text/event-stream, got %s", resp.Header.Get("Content-Type"))
	}

	// Send an update in a separate goroutine
	go func() {
		time.Sleep(100 * time.Millisecond)
		sendUpdate(updateInfo{
			Resource:  "test",
			Operation: "create",
			Data:      map[string]string{"foo": "bar"},
		})
	}()

	reader := bufio.NewReader(resp.Body)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("Failed to read from stream: %v", err)
	}

	if !strings.HasPrefix(line, "data: ") {
		t.Fatalf("Expected data prefix, got %q", line)
	}

	data := strings.TrimPrefix(strings.TrimSpace(line), "data: ")
	var update updateInfo
	if err := json.Unmarshal([]byte(data), &update); err != nil {
		t.Fatalf("Failed to unmarshal update: %v", err)
	}

	if update.Resource != "test" {
		t.Errorf("Expected resource test, got %s", update.Resource)
	}
}

func TestConcurrentUpdates(t *testing.T) {
	pool := setupTestDB(t)
	defer pool.Close()

	server := New(pool, true) // noAuth = true
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	const numClients = 5
	var wg sync.WaitGroup
	wg.Add(numClients)

	readyWg := sync.WaitGroup{}
	readyWg.Add(numClients)

	for i := 0; i < numClients; i++ {
		go func(id int) {
			defer wg.Done()
			resp, err := http.Get(ts.URL + "/api/v1/updates")
			if err != nil {
				t.Errorf("Client %d failed to connect: %v", id, err)
				readyWg.Done()
				return
			}
			defer resp.Body.Close()

			reader := bufio.NewReader(resp.Body)
			readyWg.Done()

			// Wait for the update
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					return // Connection closed
				}
				if strings.HasPrefix(line, "data: ") {
					data := strings.TrimPrefix(strings.TrimSpace(line), "data: ")
					var update updateInfo
					json.Unmarshal([]byte(data), &update)
					if update.Resource == "concurrentTest" {
						return // Success
					}
				}
			}
		}(i)
	}

	// Give clients time to connect
	readyWg.Wait()
	time.Sleep(200 * time.Millisecond)

	sendUpdate(updateInfo{
		Resource:  "concurrentTest",
		Operation: "create",
		Data:      "broadcast",
	})

	// Wait for all clients to receive the update or timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All good
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for concurrent updates")
	}
}
