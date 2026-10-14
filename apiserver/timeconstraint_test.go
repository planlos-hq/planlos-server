package apiserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTimeConstraintEndpoints(t *testing.T) {
	pool := setupTestDB(t)
	defer pool.Close()

	server := New(pool, true) // noAuth = true
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	var lecturerID int
	var constraintID int

	t.Run("SetupLecturer", func(t *testing.T) {
		lecturerData := CreateLecturerRequest{
			FirstName:    ptr("Max"),
			LastName:     ptr("Mustermann"),
			Abbreviation: ptr("MM"),
			Kind:         ptr("regular"),
		}
		body, _ := json.Marshal(lecturerData)
		resp, err := http.Post(ts.URL+"/api/v1/lecturer", "application/json", bytes.NewBuffer(body))
		if err != nil || resp.StatusCode != http.StatusCreated {
			t.Fatalf("Failed to create lecturer: %v, status: %d", err, resp.StatusCode)
		}
		var createdLecturer Lecturer
		json.NewDecoder(resp.Body).Decode(&createdLecturer)
		lecturerID = createdLecturer.ID
		resp.Body.Close()
	})

	t.Run("CreateTimeConstraint", func(t *testing.T) {
		constraintData := CreateLecturerNegativeTimeConstraintRequest{
			Lecturer: ptr(lecturerID),
			Weekday:  ptr(1),
			At:       ptr("08:00:00"),
			Duration: ptr("01:30:00"),
			Soft:     ptr(false),
		}
		body, _ := json.Marshal(constraintData)
		resp, err := http.Post(ts.URL+"/api/v1/lecturer-negative-time-constraint", "application/json", bytes.NewBuffer(body))
		if err != nil {
			t.Fatalf("Failed to send POST request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			t.Errorf("Expected status 201 Created, got %d", resp.StatusCode)
		}

		var createdConstraint LecturerNegativeTimeConstraint
		if err := json.NewDecoder(resp.Body).Decode(&createdConstraint); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}
		constraintID = createdConstraint.ID
	})

	t.Run("GetTimeConstraint", func(t *testing.T) {
		resp, err := http.Get(fmt.Sprintf("%s/api/v1/lecturer-negative-time-constraint/%d", ts.URL, constraintID))
		if err != nil {
			t.Fatalf("Failed to send GET request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 OK, got %d", resp.StatusCode)
		}
	})

	t.Run("UpdateTimeConstraint", func(t *testing.T) {
		updateData := PatchLecturerNegativeTimeConstraintRequest{
			Lecturer: ptr(lecturerID),
			Weekday:  ptr(1),
			At:       ptr("09:00:00"),
			Duration: ptr("02:00:00"),
			Soft:     ptr(true),
		}
		body, _ := json.Marshal(updateData)
		req, _ := http.NewRequest(http.MethodPut, fmt.Sprintf("%s/api/v1/lecturer-negative-time-constraint/%d", ts.URL, constraintID), bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to send PUT request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 OK, got %d", resp.StatusCode)
		}
	})

	t.Run("GetLecturerConstraints", func(t *testing.T) {
		resp, err := http.Get(fmt.Sprintf("%s/api/v1/lecturer/%d/negative-time-constraint", ts.URL, lecturerID))
		if err != nil {
			t.Fatalf("Failed to send GET request: %v", err)
		}
		defer resp.Body.Close()

		var constraints []LecturerNegativeTimeConstraint
		json.NewDecoder(resp.Body).Decode(&constraints)
		if len(constraints) == 0 {
			t.Errorf("Expected at least 1 constraint, got 0")
		}
	})

	t.Run("DeleteTimeConstraint", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/v1/lecturer-negative-time-constraint/%d", ts.URL, constraintID), nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to send DELETE request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 OK, got %d", resp.StatusCode)
		}
	})
}
