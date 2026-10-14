package apiserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLecturerEndpoints(t *testing.T) {
	pool := setupTestDB(t)
	defer pool.Close()

	server := New(pool, true) // noAuth = true
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	var lecturerID int

	t.Run("CreateLecturer", func(t *testing.T) {
		lecturerData := CreateLecturerRequest{
			FirstName:    ptr("Max"),
			LastName:     ptr("Mustermann"),
			Abbreviation: ptr("MM"),
			Kind:         ptr("regular"),
			Notes:        ptr("Test Note"),
			IsRelevant:   ptr(true),
		}
		body, _ := json.Marshal(lecturerData)
		resp, err := http.Post(ts.URL+"/api/v1/lecturer", "application/json", bytes.NewBuffer(body))
		if err != nil {
			t.Fatalf("Failed to send POST request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			t.Errorf("Expected status 201 Created, got %d", resp.StatusCode)
		}

		var createdLecturer Lecturer
		if err := json.NewDecoder(resp.Body).Decode(&createdLecturer); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}
		if *createdLecturer.LastName != "Mustermann" {
			t.Errorf("Expected last name Mustermann, got %s", *createdLecturer.LastName)
		}
		lecturerID = createdLecturer.ID
	})

	t.Run("CreateLecturerInvalidKind", func(t *testing.T) {
		lecturerData := CreateLecturerRequest{
			FirstName:    ptr("Invalid"),
			LastName:     ptr("Kind"),
			Abbreviation: ptr("IK"),
			Kind:         ptr("not-a-valid-kind"),
		}
		body, _ := json.Marshal(lecturerData)
		resp, err := http.Post(ts.URL+"/api/v1/lecturer", "application/json", bytes.NewBuffer(body))
		if err != nil {
			t.Fatalf("Failed to send POST request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status 400 Bad Request for invalid kind, got %d", resp.StatusCode)
		}
	})

	t.Run("GetLecturer", func(t *testing.T) {
		resp, err := http.Get(fmt.Sprintf("%s/api/v1/lecturer/%d", ts.URL, lecturerID))
		if err != nil {
			t.Fatalf("Failed to send GET request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 OK, got %d", resp.StatusCode)
		}

		var lecturer Lecturer
		json.NewDecoder(resp.Body).Decode(&lecturer)
		if lecturer.ID != lecturerID {
			t.Errorf("Expected ID %d, got %d", lecturerID, lecturer.ID)
		}
	})

	t.Run("UpdateLecturer", func(t *testing.T) {
		updateData := PatchLecturerRequest{
			FirstName:    ptr("Maximilian"),
			LastName:     ptr("Mustermann-Updated"),
			Abbreviation: ptr("MMU"),
			Kind:         ptr("lab_engineer"),
			Notes:        ptr("Updated Note"),
			IsRelevant:   ptr(false),
		}
		body, _ := json.Marshal(updateData)
		req, _ := http.NewRequest(http.MethodPut, fmt.Sprintf("%s/api/v1/lecturer/%d", ts.URL, lecturerID), bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to send PUT request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 OK, got %d", resp.StatusCode)
		}

		var updatedLecturer Lecturer
		json.NewDecoder(resp.Body).Decode(&updatedLecturer)
		if *updatedLecturer.FirstName != "Maximilian" {
			t.Errorf("Expected updated first name Maximilian, got %s", *updatedLecturer.FirstName)
		}
	})

	t.Run("GetLecturers", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/v1/lecturer")
		if err != nil {
			t.Fatalf("Failed to send GET request: %v", err)
		}
		defer resp.Body.Close()

		var lecturers []Lecturer
		json.NewDecoder(resp.Body).Decode(&lecturers)
		if len(lecturers) == 0 {
			t.Errorf("Expected at least 1 lecturer, got 0")
		}
	})

	t.Run("DeleteLecturer", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/v1/lecturer/%d", ts.URL, lecturerID), nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to send DELETE request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 OK, got %d", resp.StatusCode)
		}

		// Verify deletion
		respGet, _ := http.Get(fmt.Sprintf("%s/api/v1/lecturer/%d", ts.URL, lecturerID))
		if respGet.StatusCode != http.StatusNotFound {
			t.Errorf("Expected status 404 Not Found after deletion, got %d", respGet.StatusCode)
		}
	})
}
