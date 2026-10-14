package apiserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGlobalGalaktischEndpoint(t *testing.T) {
	pool := setupTestDB(t)
	defer pool.Close()

	server := New(pool, true) // noAuth = true
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	t.Run("CreateInitialData", func(t *testing.T) {
		courseData := CreateCourseRequest{
			Number:       ptr("GG-C1"),
			Name:         ptr("GG Course"),
			Abbreviation: ptr("GGC"),
		}
		body, _ := json.Marshal(courseData)
		resp, err := http.Post(ts.URL+"/api/v1/course", "application/json", bytes.NewBuffer(body))
		if err != nil || resp.StatusCode != http.StatusCreated {
			t.Fatalf("Failed to create course for GG test: %v, status: %d", err, resp.StatusCode)
		}
		resp.Body.Close()

		roomData := CreateRoomRequest{
			Number: ptr("GG-R1"),
			Name:   ptr("GG Room"),
		}
		body, _ = json.Marshal(roomData)
		resp, err = http.Post(ts.URL+"/api/v1/room", "application/json", bytes.NewBuffer(body))
		if err != nil || resp.StatusCode != http.StatusCreated {
			t.Fatalf("Failed to create room for GG test: %v, status: %d", err, resp.StatusCode)
		}
		resp.Body.Close()
	})

	t.Run("GetGlobalGalaktisch", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/v1/global-galaktisch")
		if err != nil {
			t.Fatalf("Failed to send GET request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 OK, got %d", resp.StatusCode)
		}

		var gg GlobalGalaktisch
		if err := json.NewDecoder(resp.Body).Decode(&gg); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		foundCourse := false
		for _, c := range gg.Courses {
			if c.Number != nil && *c.Number == "GG-C1" {
				foundCourse = true
				break
			}
		}
		if !foundCourse {
			t.Error("Expected to find course GG-C1 in global-galaktisch response")
		}

		foundRoom := false
		for _, r := range gg.Rooms {
			if r.Number != nil && *r.Number == "GG-R1" {
				foundRoom = true
				break
			}
		}
		if !foundRoom {
			t.Error("Expected to find room GG-R1 in global-galaktisch response")
		}
	})
}
