package apiserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCourseStudyGroupEndpoints(t *testing.T) {
	pool := setupTestDB(t)
	defer pool.Close()

	server := New(pool, true) // noAuth = true
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	var courseID int
	var studyGroupID int
	var degreeProgramID int

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

	t.Run("SetupDegreeProgram", func(t *testing.T) {
		dpData := CreateDegreeProgramRequest{
			Name:         ptr("Computer Science"),
			Abbreviation: ptr("CS"),
		}
		body, _ := json.Marshal(dpData)
		resp, err := http.Post(ts.URL+"/api/v1/degree-program", "application/json", bytes.NewBuffer(body))
		if err != nil {
			t.Fatalf("Failed to send POST request: %v", err)
		}
		defer resp.Body.Close()

		var createdDP DegreeProgram
		json.NewDecoder(resp.Body).Decode(&createdDP)
		degreeProgramID = createdDP.ID
	})

	t.Run("SetupStudyGroup", func(t *testing.T) {
		sgData := CreateStudyGroupRequest{
			StudyGroupBasicData: StudyGroupBasicData{
				SemesterSeason: ptr("winter"),
				SemesterYear:   ptr(2024),
				StudentCount:   ptr(40),
				DegreeProgram:  ptr(degreeProgramID),
			},
		}
		body, _ := json.Marshal(sgData)
		resp, err := http.Post(ts.URL+"/api/v1/study-group", "application/json", bytes.NewBuffer(body))
		if err != nil {
			t.Fatalf("Failed to send POST request: %v", err)
		}
		defer resp.Body.Close()

		var createdSG StudyGroup
		json.NewDecoder(resp.Body).Decode(&createdSG)
		studyGroupID = createdSG.ID
	})

	t.Run("CreateCourseStudyGroupAssignment", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/v1/course/%d/study-group/%d", ts.URL, courseID, studyGroupID)
		resp, err := http.Post(url, "application/json", nil)
		if err != nil {
			t.Fatalf("Failed to send POST request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			t.Errorf("Expected status 201 Created, got %d", resp.StatusCode)
		}
	})

	t.Run("GetCourseCourseStudyGroups", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/v1/course/%d/study-groups", ts.URL, courseID)
		resp, err := http.Get(url)
		if err != nil {
			t.Fatalf("Failed to send GET request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 OK, got %d", resp.StatusCode)
		}

		var studyGroups []StudyGroup
		json.NewDecoder(resp.Body).Decode(&studyGroups)
		if len(studyGroups) != 1 {
			t.Errorf("Expected 1 study group assignment, got %d", len(studyGroups))
		}
	})

	t.Run("GetCourseStudyGroups", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/v1/course/study-groups")
		if err != nil {
			t.Fatalf("Failed to send GET request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 OK, got %d", resp.StatusCode)
		}

		var assignments []CourseStudyGroup
		json.NewDecoder(resp.Body).Decode(&assignments)
		if len(assignments) != 1 {
			t.Errorf("Expected 1 assignment, got %d", len(assignments))
		}
	})

	t.Run("DeleteCourseStudyGroupAssignment", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/v1/course/%d/study-group/%d", ts.URL, courseID, studyGroupID)
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
		urlGet := fmt.Sprintf("%s/api/v1/course/%d/study-groups", ts.URL, courseID)
		respGet, _ := http.Get(urlGet)
		var studyGroups []StudyGroup
		json.NewDecoder(respGet.Body).Decode(&studyGroups)
		if len(studyGroups) != 0 {
			t.Errorf("Expected 0 study group assignments after deletion, got %d", len(studyGroups))
		}
	})
}
