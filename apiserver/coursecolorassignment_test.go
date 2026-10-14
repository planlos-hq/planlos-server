package apiserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCourseColorAssignmentEndpoints(t *testing.T) {
	pool := setupTestDB(t)
	defer pool.Close()

	server := New(pool, true) // noAuth = true
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	var courseID int
	var colorID int

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

		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("Expected status 201 Created, got %d", resp.StatusCode)
		}

		var createdCourse Course
		if err := json.NewDecoder(resp.Body).Decode(&createdCourse); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}
		courseID = createdCourse.ID
	})

	t.Run("SetupCourseColor", func(t *testing.T) {
		colorData := CreateCourseColorRequest{
			Name: ptr("Test Color"),
			Hex:  ptr("#FF0000"),
		}
		body, _ := json.Marshal(colorData)
		resp, err := http.Post(ts.URL+"/api/v1/course-color", "application/json", bytes.NewBuffer(body))
		if err != nil {
			t.Fatalf("Failed to send POST request: %v", err)
		}
		defer resp.Body.Close()

		var createdColor CourseColor
		json.NewDecoder(resp.Body).Decode(&createdColor)
		colorID = createdColor.ID
	})

	t.Run("CreateCourseColorAssignment", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/v1/course/%d/color/%d", ts.URL, courseID, colorID)
		resp, err := http.Post(url, "application/json", nil)
		if err != nil {
			t.Fatalf("Failed to send POST request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			t.Errorf("Expected status 201 Created, got %d", resp.StatusCode)
		}
	})

	t.Run("GetCourseCourseColorAssignments", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/v1/course/%d/colors", ts.URL, courseID)
		resp, err := http.Get(url)
		if err != nil {
			t.Fatalf("Failed to send GET request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 OK, got %d", resp.StatusCode)
		}

		var colors []CourseColor
		json.NewDecoder(resp.Body).Decode(&colors)
		if len(colors) != 1 {
			t.Errorf("Expected 1 color assignment, got %d", len(colors))
		}
		if colors[0].ID != colorID {
			t.Errorf("Expected color ID %d, got %d", colorID, colors[0].ID)
		}
	})

	t.Run("GetAllCourseColorAssignments", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/v1/course/colors")
		if err != nil {
			t.Fatalf("Failed to send GET request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 OK, got %d", resp.StatusCode)
		}

		var assignments []CourseColorAssignment
		json.NewDecoder(resp.Body).Decode(&assignments)
		if len(assignments) != 1 {
			t.Errorf("Expected 1 assignment, got %d", len(assignments))
		}
	})

	t.Run("DeleteCourseColorAssignment", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/v1/course/%d/color/%d", ts.URL, courseID, colorID)
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
		urlGet := fmt.Sprintf("%s/api/v1/course/%d/colors", ts.URL, courseID)
		respGet, _ := http.Get(urlGet)
		var colors []CourseColor
		json.NewDecoder(respGet.Body).Decode(&colors)
		if len(colors) != 0 {
			t.Errorf("Expected 0 color assignments after deletion, got %d", len(colors))
		}
	})
}
