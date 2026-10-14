package apiserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLecturerTimeConstraintViolationEndpoints(t *testing.T) {
	pool := setupTestDB(t)
	defer pool.Close()

	server := New(pool, true) // noAuth = true
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	var lecturerID int
	var courseID int
	var constraintID int
	var timeSlotID int

	t.Run("SetupData", func(t *testing.T) {
		// 1. Create Lecturer
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

		// 2. Create Course
		courseData := CreateCourseRequest{
			Number:       ptr("C1"),
			Name:         ptr("Course 1"),
			Abbreviation: ptr("C1"),
		}
		body, _ = json.Marshal(courseData)
		resp, err = http.Post(ts.URL+"/api/v1/course", "application/json", bytes.NewBuffer(body))
		if err != nil || resp.StatusCode != http.StatusCreated {
			t.Fatalf("Failed to create course: %v, status: %d", err, resp.StatusCode)
		}
		var createdCourse Course
		json.NewDecoder(resp.Body).Decode(&createdCourse)
		courseID = createdCourse.ID
		resp.Body.Close()

		// 3. Link Lecturer to Course
		resp, err = http.Post(fmt.Sprintf("%s/api/v1/course/%d/lecturer/%d", ts.URL, courseID, lecturerID), "application/json", nil)
		if err != nil || resp.StatusCode != http.StatusCreated {
			t.Fatalf("Failed to link lecturer to course: %v, status: %d", err, resp.StatusCode)
		}
		resp.Body.Close()

		// 4. Create Negative Time Constraint (Monday 08:00)
		constraintData := CreateLecturerNegativeTimeConstraintRequest{
			Lecturer: ptr(lecturerID),
			Weekday:  ptr(1),
			At:       ptr("08:00:00"),
			Duration: ptr("01:30:00"),
			Soft:     ptr(false),
		}
		body, _ = json.Marshal(constraintData)
		resp, err = http.Post(ts.URL+"/api/v1/lecturer-negative-time-constraint", "application/json", bytes.NewBuffer(body))
		if err != nil || resp.StatusCode != http.StatusCreated {
			t.Fatalf("Failed to create constraint: %v, status: %d", err, resp.StatusCode)
		}
		var createdConstraint LecturerNegativeTimeConstraint
		json.NewDecoder(resp.Body).Decode(&createdConstraint)
		constraintID = createdConstraint.ID
		resp.Body.Close()

		// 5. Create Time Slot (Monday 08:00)
		timeSlotData := CreateCourseTimeSlotRequest{
			Course:   ptr(courseID),
			Weekday:  ptr(1),
			At:       ptr("08:00:00"),
			Duration: ptr("01:30:00"),
		}
		body, _ = json.Marshal(timeSlotData)
		resp, err = http.Post(ts.URL+"/api/v1/course-time-slot", "application/json", bytes.NewBuffer(body))
		if err != nil || resp.StatusCode != http.StatusCreated {
			t.Fatalf("Failed to create time slot: %v, status: %d", err, resp.StatusCode)
		}
		var createdTimeSlot CourseTimeSlot
		json.NewDecoder(resp.Body).Decode(&createdTimeSlot)
		timeSlotID = createdTimeSlot.ID
		resp.Body.Close()
	})

	t.Run("GetViolations", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/v1/lecturer-time-constraint-violations")
		if err != nil {
			t.Fatalf("Failed to send GET request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 OK, got %d", resp.StatusCode)
		}

		var violations []LecturerTimeConstraintViolation
		json.NewDecoder(resp.Body).Decode(&violations)
		if len(violations) == 0 {
			t.Errorf("Expected at least 1 violation, got 0")
		}
	})

	t.Run("GetViolationsByLecturer", func(t *testing.T) {
		resp, err := http.Get(fmt.Sprintf("%s/api/v1/lecturer/%d/time-constraint-violations", ts.URL, lecturerID))
		if err != nil {
			t.Fatalf("Failed to send GET request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 OK, got %d", resp.StatusCode)
		}

		var violations []LecturerTimeConstraintViolation
		json.NewDecoder(resp.Body).Decode(&violations)
		if len(violations) == 0 {
			t.Errorf("Expected at least 1 violation, got 0")
		}
	})

	t.Run("IgnoreViolation", func(t *testing.T) {
		ignoreData := IgnoreLecturerConstraintViolationRequest{
			ConstraintID: ptr(constraintID),
			TimeSlotID:   ptr(timeSlotID),
		}
		body, _ := json.Marshal(ignoreData)
		resp, err := http.Post(ts.URL+"/api/v1/lecturer-time-constraint-violations/ignore", "application/json", bytes.NewBuffer(body))
		if err != nil {
			t.Fatalf("Failed to send POST request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			t.Errorf("Expected status 201 Created, got %d", resp.StatusCode)
		}
	})

	t.Run("GetIgnoredViolations", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/v1/lecturer-time-constraint-violations/ignored")
		if err != nil {
			t.Fatalf("Failed to send GET request: %v", err)
		}
		defer resp.Body.Close()

		var ignored []IgnoredLecturerConstraintViolation
		json.NewDecoder(resp.Body).Decode(&ignored)
		if len(ignored) == 0 {
			t.Errorf("Expected at least 1 ignored violation, got 0")
		}
	})

	t.Run("UnignoreViolation", func(t *testing.T) {
		unignoreData := IgnoreLecturerConstraintViolationRequest{
			ConstraintID: ptr(constraintID),
			TimeSlotID:   ptr(timeSlotID),
		}
		body, _ := json.Marshal(unignoreData)
		req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/lecturer-time-constraint-violations/ignore", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
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
