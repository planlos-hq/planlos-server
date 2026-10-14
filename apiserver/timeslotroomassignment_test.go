package apiserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTimeSlotRoomAssignmentEndpoints(t *testing.T) {
	pool := setupTestDB(t)
	defer pool.Close()

	server := New(pool, true) // noAuth = true
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	var courseID int
	var roomID int
	var timeSlotID int

	t.Run("SetupData", func(t *testing.T) {
		// 1. Create Course
		courseData := CreateCourseRequest{
			Number:       ptr("TSR-C1"),
			Name:         ptr("TSR Course"),
			Abbreviation: ptr("TSRC"),
		}
		body, _ := json.Marshal(courseData)
		resp, err := http.Post(ts.URL+"/api/v1/course", "application/json", bytes.NewBuffer(body))
		if err != nil || resp.StatusCode != http.StatusCreated {
			t.Fatalf("Failed to create course: %v, status: %d", err, resp.StatusCode)
		}
		var createdCourse Course
		json.NewDecoder(resp.Body).Decode(&createdCourse)
		courseID = createdCourse.ID
		resp.Body.Close()

		// 2. Create Room
		roomData := CreateRoomRequest{
			Number: ptr("TSR-R1"),
			Name:   ptr("TSR Room"),
		}
		body, _ = json.Marshal(roomData)
		resp, err = http.Post(ts.URL+"/api/v1/room", "application/json", bytes.NewBuffer(body))
		if err != nil || resp.StatusCode != http.StatusCreated {
			t.Fatalf("Failed to create room: %v, status: %d", err, resp.StatusCode)
		}
		var createdRoom Room
		json.NewDecoder(resp.Body).Decode(&createdRoom)
		roomID = createdRoom.ID
		resp.Body.Close()

		// 3. Create Course Time Slot
		timeSlotData := CreateCourseTimeSlotRequest{
			Course:   ptr(courseID),
			Weekday:  ptr(1),
			At:       ptr("10:00:00"),
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

	t.Run("CreateTimeSlotRoomAssignment", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/v1/course-time-slot/%d/room/%d", ts.URL, timeSlotID, roomID)
		resp, err := http.Post(url, "application/json", nil)
		if err != nil {
			t.Fatalf("Failed to send POST request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			t.Errorf("Expected status 201 Created, got %d", resp.StatusCode)
		}
	})

	t.Run("GetTimeSlotRoomAssignments", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/v1/course-time-slot/%d/rooms", ts.URL, timeSlotID)
		resp, err := http.Get(url)
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
			t.Errorf("Expected 1 room assignment, got %d", len(rooms))
		}
		if rooms[0].ID != roomID {
			t.Errorf("Expected room ID %d, got %d", roomID, rooms[0].ID)
		}
	})

	t.Run("GetTimeSlotRoomAssignmentsAll", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/v1/course-time-slot/rooms")
		if err != nil {
			t.Fatalf("Failed to send GET request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 OK, got %d", resp.StatusCode)
		}

		var assignments []TimeSlotRoomAssignment
		if err := json.NewDecoder(resp.Body).Decode(&assignments); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}
		if len(assignments) == 0 {
			t.Errorf("Expected at least 1 assignment, got 0")
		}
	})

	t.Run("DeleteTimeSlotRoomAssignment", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/v1/course-time-slot/%d/room/%d", ts.URL, timeSlotID, roomID)
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
		urlGet := fmt.Sprintf("%s/api/v1/course-time-slot/%d/rooms", ts.URL, timeSlotID)
		resp2, _ := http.Get(urlGet)
		var rooms []Room
		json.NewDecoder(resp2.Body).Decode(&rooms)
		if len(rooms) != 0 {
			t.Errorf("Expected 0 room assignments after deletion, got %d", len(rooms))
		}
		resp2.Body.Close()
	})
}
