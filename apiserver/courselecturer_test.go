package apiserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCourseLecturerEndpoints(t *testing.T) {
	pool := setupTestDB(t)
	defer pool.Close()

	server := New(pool, true) // noAuth = true
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	var courseID int
	var lecturerID int

	t.Run("SetupCourse", func(t *testing.T) {
		courseData := CreateCourseRequest{
			Number:       ptr("C101"),
			Name:         ptr("Test Course"),
			Abbreviation: ptr("TC"),
			SWS:          ptr(4.0),
			CreditPoints: ptr(5.0),
		}
		body, _ := json.Marshal(courseData)
		resp, err := http.Post(ts.URL+"/api/v1/course", "application/json", bytes.NewBuffer(body))
		if err != nil {
			t.Fatalf("Failed to send POST request: %v", err)
		}
		defer resp.Body.Close()

		var createdCourse Course
		json.NewDecoder(resp.Body).Decode(&createdCourse)
		courseID = createdCourse.ID
	})

	t.Run("SetupLecturer", func(t *testing.T) {
		lecturerData := CreateLecturerRequest{
			FirstName:    ptr("Max"),
			LastName:     ptr("Mustermann"),
			Abbreviation: ptr("MM"),
			Kind:         ptr("regular"),
		}
		body, _ := json.Marshal(lecturerData)
		resp, err := http.Post(ts.URL+"/api/v1/lecturer", "application/json", bytes.NewBuffer(body))
		if err != nil {
			t.Fatalf("Failed to send POST request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("Expected status 201 Created, got %d", resp.StatusCode)
		}

		var createdLecturer Lecturer
		if err := json.NewDecoder(resp.Body).Decode(&createdLecturer); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}
		lecturerID = createdLecturer.ID
	})

	t.Run("CreateCourseLecturerAssignment", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/v1/course/%d/lecturer/%d", ts.URL, courseID, lecturerID)
		resp, err := http.Post(url, "application/json", nil)
		if err != nil {
			t.Fatalf("Failed to send POST request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			t.Errorf("Expected status 201 Created, got %d", resp.StatusCode)
		}
	})

	t.Run("GetCourseCourseLecturers", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/v1/course/%d/lecturers", ts.URL, courseID)
		resp, err := http.Get(url)
		if err != nil {
			t.Fatalf("Failed to send GET request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 OK, got %d", resp.StatusCode)
		}

		var lecturers []Lecturer
		json.NewDecoder(resp.Body).Decode(&lecturers)
		if len(lecturers) != 1 {
			t.Errorf("Expected 1 lecturer assignment, got %d", len(lecturers))
		}
	})

	t.Run("GetCourseLecturers", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/v1/course/lecturers")
		if err != nil {
			t.Fatalf("Failed to send GET request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 OK, got %d", resp.StatusCode)
		}

		var assignments []CourseLecturer
		json.NewDecoder(resp.Body).Decode(&assignments)
		if len(assignments) != 1 {
			t.Errorf("Expected 1 assignment, got %d", len(assignments))
		}
	})

	t.Run("DeleteCourseLecturerAssignment", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/v1/course/%d/lecturer/%d", ts.URL, courseID, lecturerID)
		req, _ := http.NewRequest(http.MethodDelete, url, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to send DELETE request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 OK, got %d", resp.StatusCode)
		}

		// Verify deletion
		urlGet := fmt.Sprintf("%s/api/v1/course/%d/lecturers", ts.URL, courseID)
		respGet, _ := http.Get(urlGet)
		var lecturers []Lecturer
		json.NewDecoder(respGet.Body).Decode(&lecturers)
		if len(lecturers) != 0 {
			t.Errorf("Expected 0 lecturer assignments after deletion, got %d", len(lecturers))
		}
	})
}
