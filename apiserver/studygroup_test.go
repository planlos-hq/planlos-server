package apiserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStudyGroupEndpoints(t *testing.T) {
	pool := setupTestDB(t)
	defer pool.Close()

	server := New(pool, true) // noAuth = true
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	var degreeProgramID int
	var studyGroupID int

	t.Run("SetupDegreeProgram", func(t *testing.T) {
		dpData := CreateDegreeProgramRequest{
			Name:         ptr("Informatik"),
			Abbreviation: ptr("INF"),
		}
		body, _ := json.Marshal(dpData)
		resp, err := http.Post(ts.URL+"/api/v1/degree-program", "application/json", bytes.NewBuffer(body))
		if err != nil {
			t.Fatalf("Failed to create degree program: %v", err)
		}
		defer resp.Body.Close()

		var dp DegreeProgram
		json.NewDecoder(resp.Body).Decode(&dp)
		degreeProgramID = dp.ID
	})

	t.Run("CreateStudyGroup", func(t *testing.T) {
		sgData := CreateStudyGroupRequest{
			StudyGroupBasicData: StudyGroupBasicData{
				SemesterSeason: ptr("winter"),
				SemesterYear:   ptr(2025),
				StudentCount:   ptr(45),
				DegreeProgram:  ptr(degreeProgramID),
			},
		}
		body, _ := json.Marshal(sgData)
		resp, err := http.Post(ts.URL+"/api/v1/study-group", "application/json", bytes.NewBuffer(body))
		if err != nil {
			t.Fatalf("Failed to send POST request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			t.Errorf("Expected status 201 Created, got %d", resp.StatusCode)
		}

		var createdSG StudyGroup
		if err := json.NewDecoder(resp.Body).Decode(&createdSG); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}
		if *createdSG.SemesterSeason != "winter" {
			t.Errorf("Expected season winter, got %s", *createdSG.SemesterSeason)
		}
		studyGroupID = createdSG.ID
	})

	t.Run("GetStudyGroup", func(t *testing.T) {
		resp, err := http.Get(fmt.Sprintf("%s/api/v1/study-group/%d", ts.URL, studyGroupID))
		if err != nil {
			t.Fatalf("Failed to send GET request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 OK, got %d", resp.StatusCode)
		}

		var sg StudyGroup
		json.NewDecoder(resp.Body).Decode(&sg)
		if sg.ID != studyGroupID {
			t.Errorf("Expected ID %d, got %d", studyGroupID, sg.ID)
		}
	})

	t.Run("UpdateStudyGroup", func(t *testing.T) {
		updateData := PatchStudyGroupRequest{
			StudyGroupBasicData: StudyGroupBasicData{
				SemesterSeason: ptr("summer"),
				SemesterYear:   ptr(2026),
				StudentCount:   ptr(50),
				DegreeProgram:  ptr(degreeProgramID),
			},
		}
		body, _ := json.Marshal(updateData)
		req, _ := http.NewRequest(http.MethodPut, fmt.Sprintf("%s/api/v1/study-group/%d", ts.URL, studyGroupID), bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to send PUT request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 OK, got %d", resp.StatusCode)
		}

		var updatedSG StudyGroup
		json.NewDecoder(resp.Body).Decode(&updatedSG)
		if *updatedSG.SemesterSeason != "summer" {
			t.Errorf("Expected updated season summer, got %s", *updatedSG.SemesterSeason)
		}
	})

	t.Run("GetStudyGroups", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/v1/study-group")
		if err != nil {
			t.Fatalf("Failed to send GET request: %v", err)
		}
		defer resp.Body.Close()

		var sgs []StudyGroup
		json.NewDecoder(resp.Body).Decode(&sgs)
		if len(sgs) == 0 {
			t.Errorf("Expected at least 1 study group, got 0")
		}
	})

	t.Run("DeleteStudyGroup", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/v1/study-group/%d", ts.URL, studyGroupID), nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to send DELETE request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 OK, got %d", resp.StatusCode)
		}

		resp2, _ := http.Get(fmt.Sprintf("%s/api/v1/study-group/%d", ts.URL, studyGroupID))
		if resp2.StatusCode != http.StatusNotFound {
			t.Errorf("Expected status 404 Not Found after deletion, got %d", resp2.StatusCode)
		}
	})
}
