package apiserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCourseEndpoints(t *testing.T) {
	pool := setupTestDB(t)
	defer pool.Close()

	server := New(pool, true) // noAuth = true
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	var courseID int

	t.Run("CreateCourse", func(t *testing.T) {
		courseData := CreateCourseRequest{
			Number:       ptr("C123"),
			Name:         ptr("Software Engineering"),
			SWS:          ptr(4.0),
			CreditPoints: ptr(5.0),
			Abbreviation: ptr("SE"),
		}
		body, _ := json.Marshal(courseData)
		resp, err := http.Post(ts.URL+"/api/v1/course", "application/json", bytes.NewBuffer(body))
		if err != nil {
			t.Fatalf("Failed to send POST request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			t.Errorf("Expected status 201 Created, got %d", resp.StatusCode)
		}

		var createdCourse Course
		if err := json.NewDecoder(resp.Body).Decode(&createdCourse); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}
		if *createdCourse.Number != "C123" {
			t.Errorf("Expected course number C123, got %s", *createdCourse.Number)
		}
		courseID = createdCourse.ID
	})

	t.Run("GetCourse", func(t *testing.T) {
		resp, err := http.Get(fmt.Sprintf("%s/api/v1/course/%d", ts.URL, courseID))
		if err != nil {
			t.Fatalf("Failed to send GET request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 OK, got %d", resp.StatusCode)
		}

		var course Course
		json.NewDecoder(resp.Body).Decode(&course)
		if course.ID != courseID {
			t.Errorf("Expected ID %d, got %d", courseID, course.ID)
		}
	})

	t.Run("UpdateCourse", func(t *testing.T) {
		updateData := PatchCourseRequest{
			Number:       ptr("C123-Updated"),
			Name:         ptr("Advanced SE"),
			SWS:          ptr(6.0),
			CreditPoints: ptr(8.0),
			Abbreviation: ptr("ASE"),
		}
		body, _ := json.Marshal(updateData)
		req, _ := http.NewRequest(http.MethodPut, fmt.Sprintf("%s/api/v1/course/%d", ts.URL, courseID), bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to send PUT request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 OK, got %d", resp.StatusCode)
		}

		var updatedCourse Course
		json.NewDecoder(resp.Body).Decode(&updatedCourse)
		if *updatedCourse.Number != "C123-Updated" {
			t.Errorf("Expected updated number C123-Updated, got %s", *updatedCourse.Number)
		}
	})

	t.Run("GetCourses", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/v1/course")
		if err != nil {
			t.Fatalf("Failed to send GET request: %v", err)
		}
		defer resp.Body.Close()

		var courses []Course
		json.NewDecoder(resp.Body).Decode(&courses)
		if len(courses) == 0 {
			t.Errorf("Expected at least 1 course, got 0")
		}
	})

	t.Run("DeleteCourse", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/v1/course/%d", ts.URL, courseID), nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to send DELETE request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 OK, got %d", resp.StatusCode)
		}

		resp2, _ := http.Get(fmt.Sprintf("%s/api/v1/course/%d", ts.URL, courseID))
		if resp2.StatusCode != http.StatusNotFound {
			t.Errorf("Expected status 404 Not Found after deletion, got %d", resp2.StatusCode)
		}
	})
}
