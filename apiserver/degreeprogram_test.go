package apiserver

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDegreeProgramEndpoints(t *testing.T) {
	pool := setupTestDB(t)
	defer pool.Close()

	server := New(pool, true) // noAuth = true
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	var degreeProgramID int

	t.Run("CreateDegreeProgram", func(t *testing.T) {
		dpData := CreateDegreeProgramRequest{
			Name:         ptr("Informatik"),
			Abbreviation: ptr("INF"),
		}
		body, _ := json.Marshal(dpData)
		resp, err := http.Post(ts.URL+"/api/v1/degree-program", "application/json", bytes.NewBuffer(body))
		if err != nil {
			t.Fatalf("Failed to send POST request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			t.Errorf("Expected status 201 Created, got %d", resp.StatusCode)
		}

		var createdDP DegreeProgram
		if err := json.NewDecoder(resp.Body).Decode(&createdDP); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}
		if *createdDP.Name != "Informatik" {
			t.Errorf("Expected name Informatik, got %s", *createdDP.Name)
		}
		degreeProgramID = createdDP.ID
	})

	t.Run("GetDegreeProgram", func(t *testing.T) {
		resp, err := http.Get(fmt.Sprintf("%s/api/v1/degree-program/%d", ts.URL, degreeProgramID))
		if err != nil {
			t.Fatalf("Failed to send GET request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 OK, got %d", resp.StatusCode)
		}

		var dp DegreeProgram
		json.NewDecoder(resp.Body).Decode(&dp)
		if dp.ID != degreeProgramID {
			t.Errorf("Expected ID %d, got %d", degreeProgramID, dp.ID)
		}
	})

	t.Run("UpdateDegreeProgram", func(t *testing.T) {
		updateData := PatchDegreeProgramRequest{
			Name:         ptr("Informatik Updated"),
			Abbreviation: ptr("INFU"),
		}
		body, _ := json.Marshal(updateData)
		req, _ := http.NewRequest(http.MethodPut, fmt.Sprintf("%s/api/v1/degree-program/%d", ts.URL, degreeProgramID), bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to send PUT request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 OK, got %d", resp.StatusCode)
		}

		var updatedDP DegreeProgram
		json.NewDecoder(resp.Body).Decode(&updatedDP)
		if *updatedDP.Name != "Informatik Updated" {
			t.Errorf("Expected updated name Informatik Updated, got %s", *updatedDP.Name)
		}
	})

	t.Run("GetDegreePrograms", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/v1/degree-program")
		if err != nil {
			t.Fatalf("Failed to send GET request: %v", err)
		}
		defer resp.Body.Close()

		var dps []DegreeProgram
		json.NewDecoder(resp.Body).Decode(&dps)
		if len(dps) == 0 {
			t.Errorf("Expected at least 1 degree program, got 0")
		}
	})

	t.Run("DeleteDegreeProgram", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/v1/degree-program/%d", ts.URL, degreeProgramID), nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to send DELETE request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 OK, got %d", resp.StatusCode)
		}

		resp2, _ := http.Get(fmt.Sprintf("%s/api/v1/degree-program/%d", ts.URL, degreeProgramID))
		if resp2.StatusCode != http.StatusNotFound {
			t.Errorf("Expected status 404 Not Found after deletion, got %d", resp2.StatusCode)
		}
	})
}

func TestDegreeProgramUpdates(t *testing.T) {
	pool := setupTestDB(t)
	defer pool.Close()

	server := New(pool, true) // noAuth = true
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	// Connect to /updates
	resp, err := http.Get(ts.URL + "/api/v1/updates")
	if err != nil {
		t.Fatalf("Failed to connect to /updates: %v", err)
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)

	// Function to read next update from SSE
	readUpdate := func() updateInfo {
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatalf("Failed to read from stream: %v", err)
			}
			if strings.HasPrefix(line, "data: ") {
				data := strings.TrimPrefix(strings.TrimSpace(line), "data: ")
				var update updateInfo
				if err := json.Unmarshal([]byte(data), &update); err != nil {
					t.Fatalf("Failed to unmarshal update: %v", err)
				}
				return update
			}
		}
	}

	var degreeProgramID int

	t.Run("CreateUpdate", func(t *testing.T) {
		dpData := CreateDegreeProgramRequest{
			Name:         ptr("UpdateTest"),
			Abbreviation: ptr("UT"),
		}
		body, _ := json.Marshal(dpData)
		resp, err := http.Post(ts.URL+"/api/v1/degree-program", "application/json", bytes.NewBuffer(body))
		if err != nil {
			t.Fatalf("Failed to send POST request: %v", err)
		}
		var createdDP DegreeProgram
		json.NewDecoder(resp.Body).Decode(&createdDP)
		degreeProgramID = createdDP.ID

		update := readUpdate()
		if update.Resource != "degreeProgram" {
			t.Errorf("Expected resource degreeProgram, got %s", update.Resource)
		}
		if update.Operation != createOperation {
			t.Errorf("Expected operation create, got %s", update.Operation)
		}
		// Data should be the created degree program
		dataMap, ok := update.Data.(map[string]any)
		if !ok {
			t.Fatalf("Expected update data to be a map, got %T", update.Data)
		}
		if int(dataMap["id"].(float64)) != degreeProgramID {
			t.Errorf("Expected ID %d in update data, got %v", degreeProgramID, dataMap["id"])
		}
	})

	t.Run("UpdateUpdate", func(t *testing.T) {
		updateData := PatchDegreeProgramRequest{
			Name: ptr("UpdateTest Updated"),
		}
		body, _ := json.Marshal(updateData)
		req, _ := http.NewRequest(http.MethodPut, fmt.Sprintf("%s/api/v1/degree-program/%d", ts.URL, degreeProgramID), bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		_, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to send PUT request: %v", err)
		}

		update := readUpdate()
		if update.Resource != "degreeProgram" {
			t.Errorf("Expected resource degreeProgram, got %s", update.Resource)
		}
		if update.Operation != updateOperation {
			t.Errorf("Expected operation update, got %s", update.Operation)
		}
		dataMap, ok := update.Data.(map[string]any)
		if !ok {
			t.Fatalf("Expected update data to be a map, got %T", update.Data)
		}
		if int(dataMap["id"].(float64)) != degreeProgramID {
			t.Errorf("Expected ID %d in update data, got %v", degreeProgramID, dataMap["id"])
		}
		if dataMap["name"].(string) != "UpdateTest Updated" {
			t.Errorf("Expected updated name, got %s", dataMap["name"])
		}
	})

	t.Run("DeleteUpdate", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/v1/degree-program/%d", ts.URL, degreeProgramID), nil)
		_, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to send DELETE request: %v", err)
		}

		update := readUpdate()
		if update.Resource != "degreeProgram" {
			t.Errorf("Expected resource degreeProgram, got %s", update.Resource)
		}
		if update.Operation != deletionOperation {
			t.Errorf("Expected operation delete, got %s", update.Operation)
		}
		// Data should be the ID of the deleted degree program
		if int(update.Data.(float64)) != degreeProgramID {
			t.Errorf("Expected deleted ID %d in update data, got %v", degreeProgramID, update.Data)
		}
	})
}

func TestDegreeProgramDeletionConflict(t *testing.T) {
	pool := setupTestDB(t)
	defer pool.Close()

	server := New(pool, true) // noAuth = true
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	// 1. Create Degree Program
	dpData := CreateDegreeProgramRequest{
		Name:         ptr("ConflictTest"),
		Abbreviation: ptr("CT"),
	}
	body, _ := json.Marshal(dpData)
	resp, _ := http.Post(ts.URL+"/api/v1/degree-program", "application/json", bytes.NewBuffer(body))
	var createdDP DegreeProgram
	json.NewDecoder(resp.Body).Decode(&createdDP)

	// 2. Create Study Group referencing it
	sgData := CreateStudyGroupRequest{
		StudyGroupBasicData: StudyGroupBasicData{
			SemesterSeason: ptr("winter"),
			SemesterYear:   ptr(2025),
			StudentCount:   ptr(40),
			DegreeProgram:  ptr(createdDP.ID),
		},
	}
	body, _ = json.Marshal(sgData)
	http.Post(ts.URL+"/api/v1/study-group", "application/json", bytes.NewBuffer(body))

	// 3. Try to delete Degree Program
	req, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/v1/degree-program/%d", ts.URL, createdDP.ID), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to send DELETE request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Errorf("Expected status 409 Conflict, got %d", resp.StatusCode)
	}
}
