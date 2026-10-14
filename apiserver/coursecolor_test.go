package apiserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCourseColorEndpoints(t *testing.T) {
	pool := setupTestDB(t)
	defer pool.Close()

	server := New(pool, true) // noAuth = true
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	var colorID int

	t.Run("CreateCourseColor", func(t *testing.T) {
		colorData := CreateCourseColorRequest{
			Name: ptr("Red"),
			Hex:  ptr("#FF0000"),
		}
		body, _ := json.Marshal(colorData)
		resp, err := http.Post(ts.URL+"/api/v1/course-color", "application/json", bytes.NewBuffer(body))
		if err != nil {
			t.Fatalf("Failed to send POST request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			t.Errorf("Expected status 201 Created, got %d", resp.StatusCode)
		}

		var createdColor CourseColor
		if err := json.NewDecoder(resp.Body).Decode(&createdColor); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}
		if *createdColor.Hex != "#FF0000" {
			t.Errorf("Expected hex #FF0000, got %s", *createdColor.Hex)
		}
		colorID = createdColor.ID
	})

	t.Run("GetCourseColor", func(t *testing.T) {
		resp, err := http.Get(fmt.Sprintf("%s/api/v1/course-color/%d", ts.URL, colorID))
		if err != nil {
			t.Fatalf("Failed to send GET request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 OK, got %d", resp.StatusCode)
		}

		var color CourseColor
		json.NewDecoder(resp.Body).Decode(&color)
		if color.ID != colorID {
			t.Errorf("Expected ID %d, got %d", colorID, color.ID)
		}
	})

	t.Run("UpdateCourseColor", func(t *testing.T) {
		updateData := PatchCourseColorRequest{
			Name: ptr("Blue"),
			Hex:  ptr("#0000FF"),
		}
		body, _ := json.Marshal(updateData)
		req, _ := http.NewRequest(http.MethodPut, fmt.Sprintf("%s/api/v1/course-color/%d", ts.URL, colorID), bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to send PUT request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 OK, got %d", resp.StatusCode)
		}

		var updatedColor CourseColor
		json.NewDecoder(resp.Body).Decode(&updatedColor)
		if *updatedColor.Hex != "#0000FF" {
			t.Errorf("Expected updated hex #0000FF, got %s", *updatedColor.Hex)
		}
	})

	t.Run("GetCourseColors", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/v1/course-color")
		if err != nil {
			t.Fatalf("Failed to send GET request: %v", err)
		}
		defer resp.Body.Close()

		var colors []CourseColor
		json.NewDecoder(resp.Body).Decode(&colors)
		if len(colors) == 0 {
			t.Errorf("Expected at least 1 color, got 0")
		}
	})

	t.Run("DeleteCourseColor", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/v1/course-color/%d", ts.URL, colorID), nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to send DELETE request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 OK, got %d", resp.StatusCode)
		}

		resp2, _ := http.Get(fmt.Sprintf("%s/api/v1/course-color/%d", ts.URL, colorID))
		if resp2.StatusCode != http.StatusNotFound {
			t.Errorf("Expected status 404 Not Found after deletion, got %d", resp2.StatusCode)
		}
	})
}
