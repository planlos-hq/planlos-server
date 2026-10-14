package apiserver

import (
	"bytes"
	"context"
	"encoding/json"
	_ "embed"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func setupTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}

	cfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		t.Fatalf("Failed to parse DATABASE_URL: %v", err)
	}

	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Failed to connect to database: %v", err)
	}

	// Clean database
	_, err = pool.Exec(context.Background(), truncateEverythingQuery)
	if err != nil {
		t.Fatalf("Failed to truncate database: %v", err)
	}

	return pool
}

func TestRoomEndpoints(t *testing.T) {
	pool := setupTestDB(t)
	defer pool.Close()

	server := New(pool, true) // noAuth = true for testing endpoints easily
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	var roomID int

	t.Run("CreateRoom", func(t *testing.T) {
		roomData := CreateRoomRequest{
			Number: ptr("A101"),
			Name:   ptr("Lecture Hall 1"),
			Seats:  ptr(100),
		}
		body, _ := json.Marshal(roomData)
		resp, err := http.Post(ts.URL+"/api/v1/room", "application/json", bytes.NewBuffer(body))
		if err != nil {
			t.Fatalf("Failed to send POST request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			t.Errorf("Expected status 201 Created, got %d", resp.StatusCode)
		}

		var createdRoom Room
		if err := json.NewDecoder(resp.Body).Decode(&createdRoom); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}
		if *createdRoom.Number != "A101" {
			t.Errorf("Expected room number A101, got %s", *createdRoom.Number)
		}
		roomID = createdRoom.ID
	})

	t.Run("GetRoom", func(t *testing.T) {
		resp, err := http.Get(fmt.Sprintf("%s/api/v1/room/%d", ts.URL, roomID))
		if err != nil {
			t.Fatalf("Failed to send GET request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 OK, got %d", resp.StatusCode)
		}

		var room Room
		if err := json.NewDecoder(resp.Body).Decode(&room); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}
		if room.ID != roomID {
			t.Errorf("Expected room ID %d, got %d", roomID, room.ID)
		}
	})

	t.Run("UpdateRoom", func(t *testing.T) {
		updateData := PatchRoomRequest{
			Number: ptr("A101-Updated"),
			Name:   ptr("Updated Hall"),
			Seats:  ptr(120),
		}
		body, _ := json.Marshal(updateData)
		req, _ := http.NewRequest(http.MethodPut, fmt.Sprintf("%s/api/v1/room/%d", ts.URL, roomID), bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to send PUT request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 OK, got %d", resp.StatusCode)
		}

		var updatedRoom Room
		if err := json.NewDecoder(resp.Body).Decode(&updatedRoom); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}
		if *updatedRoom.Number != "A101-Updated" {
			t.Errorf("Expected updated room number A101-Updated, got %s", *updatedRoom.Number)
		}
	})

	t.Run("GetRooms", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/v1/room")
		if err != nil {
			t.Fatalf("Failed to send GET request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 OK, got %d", resp.StatusCode)
		}

		var rooms []Room
		if err := json.NewDecoder(resp.Body).Decode(&rooms); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}
		if len(rooms) != 1 {
			t.Errorf("Expected 1 room, got %d", len(rooms))
		}
	})

	t.Run("DeleteRoom", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/v1/room/%d", ts.URL, roomID), nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to send DELETE request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 OK, got %d", resp.StatusCode)
		}

		// Verify deletion
		resp2, _ := http.Get(fmt.Sprintf("%s/api/v1/room/%d", ts.URL, roomID))
		if resp2.StatusCode != http.StatusNotFound {
			t.Errorf("Expected status 404 Not Found after deletion, got %d", resp2.StatusCode)
		}
	})
}

func ptr[T any](v T) *T {
	return &v
}
