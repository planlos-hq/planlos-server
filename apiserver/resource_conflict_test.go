package apiserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResourceConflictEndpoints(t *testing.T) {
	pool := setupTestDB(t)
	defer pool.Close()

	server := New(pool, true) // noAuth = true
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	var roomID int
	var course1ID int
	var course2ID int
	var slot1ID int
	var slot2ID int

	t.Run("SetupData", func(t *testing.T) {
		// 1. Create Room
		roomData := CreateRoomRequest{
			Number: ptr("R1"),
			Name:   ptr("Room 1"),
		}
		body, _ := json.Marshal(roomData)
		resp, _ := http.Post(ts.URL+"/api/v1/room", "application/json", bytes.NewBuffer(body))
		var createdRoom Room
		json.NewDecoder(resp.Body).Decode(&createdRoom)
		roomID = createdRoom.ID
		resp.Body.Close()

		// 2. Create Course 1
		course1Data := CreateCourseRequest{Number: ptr("C1"), Name: ptr("Course 1"), Abbreviation: ptr("C1"), SWS: ptr(2.0), CreditPoints: ptr(5.0)}
		body, _ = json.Marshal(course1Data)
		resp, _ = http.Post(ts.URL+"/api/v1/course", "application/json", bytes.NewBuffer(body))
		var c1 Course
		json.NewDecoder(resp.Body).Decode(&c1)
		course1ID = c1.ID
		resp.Body.Close()

		// 3. Create Course 2
		course2Data := CreateCourseRequest{Number: ptr("C2"), Name: ptr("Course 2"), Abbreviation: ptr("C2"), SWS: ptr(2.0), CreditPoints: ptr(5.0)}
		body, _ = json.Marshal(course2Data)
		resp, _ = http.Post(ts.URL+"/api/v1/course", "application/json", bytes.NewBuffer(body))
		var c2 Course
		json.NewDecoder(resp.Body).Decode(&c2)
		course2ID = c2.ID
		resp.Body.Close()

		// 4. Create Time Slot 1 (Monday 08:00)
		slot1Data := CreateCourseTimeSlotRequest{Course: ptr(course1ID), Weekday: ptr(1), At: ptr("08:00:00"), Duration: ptr("01:30:00")}
		body, _ = json.Marshal(slot1Data)
		resp, _ = http.Post(ts.URL+"/api/v1/course-time-slot", "application/json", bytes.NewBuffer(body))
		var s1 CourseTimeSlot
		json.NewDecoder(resp.Body).Decode(&s1)
		slot1ID = s1.ID
		resp.Body.Close()

		// 5. Create Time Slot 2 (Monday 08:00)
		slot2Data := CreateCourseTimeSlotRequest{Course: ptr(course2ID), Weekday: ptr(1), At: ptr("08:00:00"), Duration: ptr("01:30:00")}
		body, _ = json.Marshal(slot2Data)
		resp, _ = http.Post(ts.URL+"/api/v1/course-time-slot", "application/json", bytes.NewBuffer(body))
		var s2 CourseTimeSlot
		json.NewDecoder(resp.Body).Decode(&s2)
		slot2ID = s2.ID
		resp.Body.Close()

		// 6. Assign Room to Slot 1
		resp, _ = http.Post(fmt.Sprintf("%s/api/v1/course-time-slot/%d/room/%d", ts.URL, slot1ID, roomID), "application/json", nil)
		resp.Body.Close()

		// 7. Assign Room to Slot 2 -> CONFLICT
		resp, _ = http.Post(fmt.Sprintf("%s/api/v1/course-time-slot/%d/room/%d", ts.URL, slot2ID, roomID), "application/json", nil)
		resp.Body.Close()
	})

	t.Run("GetConflicts", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/v1/resource-constraint-violations")
		if err != nil {
			t.Fatalf("Failed to send GET request: %v", err)
		}
		defer resp.Body.Close()

		var conflicts []ResourceConflict
		json.NewDecoder(resp.Body).Decode(&conflicts)
		if len(conflicts) == 0 {
			t.Errorf("Expected at least 1 conflict, got 0")
		}
	})

	t.Run("GetConflictsByRoom", func(t *testing.T) {
		resp, err := http.Get(fmt.Sprintf("%s/api/v1/room/%d/constraint-violations", ts.URL, roomID))
		if err != nil {
			t.Fatalf("Failed to send GET request: %v", err)
		}
		defer resp.Body.Close()

		var conflicts []ResourceConflict
		json.NewDecoder(resp.Body).Decode(&conflicts)
		if len(conflicts) == 0 {
			t.Errorf("Expected at least 1 conflict, got 0")
		}
	})

	t.Run("IgnoreConflict", func(t *testing.T) {
		ignoreData := IgnoreResourceConflictRequest{
			SlotAID:      ptr(slot1ID),
			SlotBID:      ptr(slot2ID),
			ResourceType: ptr("room"),
			ResourceID:   ptr(roomID),
		}
		body, _ := json.Marshal(ignoreData)
		resp, err := http.Post(ts.URL+"/api/v1/resource-constraint-violations/ignore", "application/json", bytes.NewBuffer(body))
		if err != nil || resp.StatusCode != http.StatusCreated {
			t.Errorf("Expected status 201 Created, got %d", resp.StatusCode)
		}
		resp.Body.Close()
	})

	t.Run("GetIgnoredConflicts", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/v1/resource-constraint-violations/ignored")
		if err != nil {
			t.Fatalf("Failed to send GET request: %v", err)
		}
		defer resp.Body.Close()

		var ignored []IgnoredResourceConflict
		json.NewDecoder(resp.Body).Decode(&ignored)
		if len(ignored) == 0 {
			t.Errorf("Expected at least 1 ignored conflict, got 0")
		}
	})

	t.Run("UnignoreConflict", func(t *testing.T) {
		unignoreData := IgnoreResourceConflictRequest{
			SlotAID:      ptr(slot1ID),
			SlotBID:      ptr(slot2ID),
			ResourceType: ptr("room"),
			ResourceID:   ptr(roomID),
		}
		body, _ := json.Marshal(unignoreData)
		req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/resource-constraint-violations/ignore", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 OK, got %d", resp.StatusCode)
		}
		resp.Body.Close()
	})
}

func TestResourceOverlapEdgeCases(t *testing.T) {
	pool := setupTestDB(t)
	defer pool.Close()

	server := New(pool, true) // noAuth = true
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	// Setup: 1 Room, 3 Courses
	roomID := createTestRoom(t, ts, "EdgeRoom")
	c1 := createTestCourse(t, ts, "C1")
	c2 := createTestCourse(t, ts, "C2")
	c3 := createTestCourse(t, ts, "C3")

	// 1. Slot 1: 08:00 - 09:30
	s1 := createTestTimeSlot(t, ts, c1, "08:00:00", "01:30:00")
	// 2. Slot 2: 09:30 - 11:00 (Exact boundary, NO overlap)
	s2 := createTestTimeSlot(t, ts, c2, "09:30:00", "01:30:00")
	// 3. Slot 3: 09:29 - 11:00 (1 minute overlap with Slot 1)
	s3 := createTestTimeSlot(t, ts, c3, "09:29:00", "01:30:00")

	assignRoom(t, ts, s1, roomID)
	assignRoom(t, ts, s2, roomID)
	assignRoom(t, ts, s3, roomID)

	t.Run("CheckNoOverlapAtBoundary", func(t *testing.T) {
		// Should NOT see conflict between s1 and s2
		resp, _ := http.Get(ts.URL + "/api/v1/resource-constraint-violations")
		var conflicts []ResourceConflict
		json.NewDecoder(resp.Body).Decode(&conflicts)
		resp.Body.Close()

		found := false
		for _, c := range conflicts {
			if (c.SlotAID == s1 && c.SlotBID == s2) || (c.SlotAID == s2 && c.SlotBID == s1) {
				found = true
			}
		}
		if found {
			t.Error("Did not expect conflict at exact time boundary (09:30)")
		}
	})

	t.Run("CheckOverlapAtOneMinute", func(t *testing.T) {
		// Should see conflict between s1 and s3
		resp, _ := http.Get(ts.URL + "/api/v1/resource-constraint-violations")
		var conflicts []ResourceConflict
		json.NewDecoder(resp.Body).Decode(&conflicts)
		resp.Body.Close()

		found := false
		for _, c := range conflicts {
			if (c.SlotAID == s1 && c.SlotBID == s3) || (c.SlotAID == s3 && c.SlotBID == s1) {
				found = true
			}
		}
		if !found {
			t.Error("Expected conflict between 08:00-09:30 and 09:29-10:59")
		}
	})
}

// Helper functions for TestResourceOverlapEdgeCases

func createTestRoom(t *testing.T, ts *httptest.Server, number string) int {
	data := CreateRoomRequest{Number: ptr(number)}
	body, _ := json.Marshal(data)
	resp, _ := http.Post(ts.URL+"/api/v1/room", "application/json", bytes.NewBuffer(body))
	var r Room
	json.NewDecoder(resp.Body).Decode(&r)
	resp.Body.Close()
	return r.ID
}

func createTestCourse(t *testing.T, ts *httptest.Server, abbr string) int {
	data := CreateCourseRequest{Number: ptr(abbr), Name: ptr(abbr), Abbreviation: ptr(abbr)}
	body, _ := json.Marshal(data)
	resp, _ := http.Post(ts.URL+"/api/v1/course", "application/json", bytes.NewBuffer(body))
	var c Course
	json.NewDecoder(resp.Body).Decode(&c)
	resp.Body.Close()
	return c.ID
}

func createTestTimeSlot(t *testing.T, ts *httptest.Server, courseID int, at, duration string) int {
	data := CreateCourseTimeSlotRequest{Course: ptr(courseID), Weekday: ptr(1), At: ptr(at), Duration: ptr(duration)}
	body, _ := json.Marshal(data)
	resp, _ := http.Post(ts.URL+"/api/v1/course-time-slot", "application/json", bytes.NewBuffer(body))
	var s CourseTimeSlot
	json.NewDecoder(resp.Body).Decode(&s)
	resp.Body.Close()
	return s.ID
}

func assignRoom(t *testing.T, ts *httptest.Server, slotID, roomID int) {
	resp, _ := http.Post(fmt.Sprintf("%s/api/v1/course-time-slot/%d/room/%d", ts.URL, slotID, roomID), "application/json", nil)
	resp.Body.Close()
}
