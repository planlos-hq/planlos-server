package apiserver

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCSVImportExport(t *testing.T) {
	pool := setupTestDB(t)
	defer pool.Close()

	server := New(pool, true)
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	t.Run("ImportRooms", func(t *testing.T) {
		csvData := "nummer,name,plätze,extra_daten\n" +
			"A101,Lecture Hall 1,100,{}\n" +
			"B202,Lab 2,20,\"{\"\"type\"\": \"\"lab\"\"}\"\n"

		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, err := writer.CreateFormFile("file", "rooms.csv")
		if err != nil {
			t.Fatalf("Failed to create form file: %v", err)
		}
		part.Write([]byte(csvData))
		writer.Close()

		resp, err := http.Post(ts.URL+"/api/v1/import/csv", writer.FormDataContentType(), body)
		if err != nil {
			t.Fatalf("Failed to send POST request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			var errResp CSVErrorResponse
			json.NewDecoder(resp.Body).Decode(&errResp)
			t.Errorf("Expected status 200 OK, got %d. Error: %+v", resp.StatusCode, errResp)
		}

		var successResp CSVSuccessResponse
		if err := json.NewDecoder(resp.Body).Decode(&successResp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}
		if successResp.ImportedEntries != 2 {
			t.Errorf("Expected 2 imported entries, got %d", successResp.ImportedEntries)
		}

		// Verify data in DB
		resp, err = http.Get(ts.URL + "/api/v1/room")
		if err != nil {
			t.Fatalf("Failed to get rooms: %v", err)
		}
		defer resp.Body.Close()
		var rooms []Room
		json.NewDecoder(resp.Body).Decode(&rooms)
		if len(rooms) != 2 {
			t.Errorf("Expected 2 rooms in DB, got %d", len(rooms))
		}
	})

	t.Run("ExportRooms", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/v1/export/csv/room")
		if err != nil {
			t.Fatalf("Failed to send GET request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 OK, got %d", resp.StatusCode)
		}

		if contentType := resp.Header.Get("Content-Type"); contentType != "text/csv; charset=utf-8" {
			t.Errorf("Expected Content-Type text/csv; charset=utf-8, got %s", contentType)
		}
	})

	t.Run("ImportLecturers", func(t *testing.T) {
		csvData := "titel,vorname,nachname,kürzel,art,notizen,extra_daten\n" +
			"Prof. Dr.,Max,Mustermann,MM,regular,Some notes,{}\n" +
			",Jane,Doe,JD,lehrbeauftragter,,\"{\"\"foo\"\": \"\"bar\"\"}\"\n"

		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, err := writer.CreateFormFile("file", "lecturers.csv")
		if err != nil {
			t.Fatalf("Failed to create form file: %v", err)
		}
		part.Write([]byte(csvData))
		writer.Close()

		resp, err := http.Post(ts.URL+"/api/v1/import/csv", writer.FormDataContentType(), body)
		if err != nil {
			t.Fatalf("Failed to send POST request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			var errResp CSVErrorResponse
			json.NewDecoder(resp.Body).Decode(&errResp)
			t.Errorf("Expected status 200 OK, got %d. Error: %+v", resp.StatusCode, errResp)
		}

		var successResp CSVSuccessResponse
		json.NewDecoder(resp.Body).Decode(&successResp)
		if successResp.ImportedEntries != 2 {
			t.Errorf("Expected 2 imported entries, got %d", successResp.ImportedEntries)
		}
	})

	t.Run("ImportInvalidCSV", func(t *testing.T) {
		csvData := "invalid,header\nfoo,bar\n"

		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, err := writer.CreateFormFile("file", "invalid.csv")
		if err != nil {
			t.Fatalf("Failed to create form file: %v", err)
		}
		part.Write([]byte(csvData))
		writer.Close()

		resp, err := http.Post(ts.URL+"/api/v1/import/csv", writer.FormDataContentType(), body)
		if err != nil {
			t.Fatalf("Failed to send POST request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("Expected status 422 Unprocessable Entity, got %d", resp.StatusCode)
		}

		var errResp CSVErrorResponse
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}
		if errResp.ErrType != "INVALID_HEADER_NAME" {
			t.Errorf("Expected error type INVALID_HEADER_NAME, got %s", errResp.ErrType)
		}
	})
}
