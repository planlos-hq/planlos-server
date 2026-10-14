package apiserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCourseTimeSlotEndpoints(t *testing.T) {
	pool := setupTestDB(t)
	defer pool.Close()

	server := New(pool, true) // noAuth = true
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	var courseID int
	var timeSlotID int

	t.Run("CreateCourseForTimeSlot", func(t *testing.T) {
		courseData := CreateCourseRequest{
			Number:       ptr("C101"),
			Name:         ptr("Programming 1"),
			SWS:          ptr(4.0),
			CreditPoints: ptr(5.0),
			Abbreviation: ptr("P1"),
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
		courseID = createdCourse.ID
	})

	t.Run("CreateCourseTimeSlot", func(t *testing.T) {
		timeSlotData := CreateCourseTimeSlotRequest{
			Course:   ptr(courseID),
			Weekday:  ptr(1), // Monday
			At:       ptr("08:00:00"),
			Duration: ptr("01:30:00"),
		}
		body, _ := json.Marshal(timeSlotData)
		resp, err := http.Post(ts.URL+"/api/v1/course-time-slot", "application/json", bytes.NewBuffer(body))
		if err != nil {
			t.Fatalf("Failed to send POST request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			t.Errorf("Expected status 201 Created, got %d", resp.StatusCode)
		}

		var createdTimeSlot CourseTimeSlot
		if err := json.NewDecoder(resp.Body).Decode(&createdTimeSlot); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}
		if *createdTimeSlot.Course != courseID {
			t.Errorf("Expected course ID %d, got %d", courseID, *createdTimeSlot.Course)
		}
		timeSlotID = createdTimeSlot.ID
	})

	t.Run("GetCourseTimeSlot", func(t *testing.T) {
		resp, err := http.Get(fmt.Sprintf("%s/api/v1/course-time-slot/%d", ts.URL, timeSlotID))
		if err != nil {
			t.Fatalf("Failed to send GET request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 OK, got %d", resp.StatusCode)
		}

		var timeSlot CourseTimeSlot
		if err := json.NewDecoder(resp.Body).Decode(&timeSlot); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}
		if timeSlot.ID != timeSlotID {
			t.Errorf("Expected ID %d, got %d", timeSlotID, timeSlot.ID)
		}
	})

	t.Run("UpdateCourseTimeSlot", func(t *testing.T) {
		updateData := PatchCourseTimeSlotRequest{
			Weekday: ptr(2), // Tuesday
			At:      ptr("10:00:00"),
		}
		body, _ := json.Marshal(updateData)
		req, _ := http.NewRequest(http.MethodPut, fmt.Sprintf("%s/api/v1/course-time-slot/%d", ts.URL, timeSlotID), bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to send PUT request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 OK, got %d", resp.StatusCode)
		}

		var updatedTimeSlot CourseTimeSlot
		if err := json.NewDecoder(resp.Body).Decode(&updatedTimeSlot); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}
		if *updatedTimeSlot.Weekday != 2 {
			t.Errorf("Expected weekday 2, got %d", *updatedTimeSlot.Weekday)
		}
	})

	t.Run("GetCourseTimeSlots", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/v1/course-time-slot")
		if err != nil {
			t.Fatalf("Failed to send GET request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 OK, got %d", resp.StatusCode)
		}

		var timeSlots []CourseTimeSlot
		if err := json.NewDecoder(resp.Body).Decode(&timeSlots); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}
		if len(timeSlots) == 0 {
			t.Errorf("Expected at least 1 course time slot, got 0")
		}
	})

	t.Run("DeleteCourseTimeSlot", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/v1/course-time-slot/%d", ts.URL, timeSlotID), nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to send DELETE request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 OK, got %d", resp.StatusCode)
		}

		// Verify deletion
		resp2, _ := http.Get(fmt.Sprintf("%s/api/v1/course-time-slot/%d", ts.URL, timeSlotID))
		if resp2.StatusCode != http.StatusNotFound {
			t.Errorf("Expected status 404 Not Found after deletion, got %d", resp2.StatusCode)
		}
	})
}
