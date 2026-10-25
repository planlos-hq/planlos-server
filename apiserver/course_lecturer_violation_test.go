package apiserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCourseLecturerViolationEndpoint(t *testing.T) {
	pool := setupTestDB(t)
	defer pool.Close()

	server := New(pool, true) // noAuth = true
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	ctx := t.Context()
	mustScan := func(dest *int, query string, args ...any) {
		t.Helper()
		if err := pool.QueryRow(ctx, query, args...).Scan(dest); err != nil {
			t.Fatalf("setup %q: %v", query, err)
		}
	}
	var labOnly, mixed, empty, lab1, lab2, regular int
	mustScan(&labOnly, `insert into course (number, name, abbreviation) values ('C1', 'Nur Labor', 'NL') returning id`)
	mustScan(&mixed, `insert into course (number, name, abbreviation) values ('C2', 'Gemischt', 'G') returning id`)
	mustScan(&empty, `insert into course (number, name, abbreviation) values ('C3', 'Ohne Dozent', 'OD') returning id`)
	mustScan(&lab1, `insert into lecturer (first_name, last_name, abbreviation, kind) values ('L', 'Eins', 'L1', 'lab_engineer') returning id`)
	mustScan(&lab2, `insert into lecturer (first_name, last_name, abbreviation, kind) values ('L', 'Zwei', 'L2', 'lab_engineer') returning id`)
	mustScan(&regular, `insert into lecturer (first_name, last_name, abbreviation, kind) values ('R', 'Prof', 'RP', 'regular') returning id`)
	if _, err := pool.Exec(ctx, `insert into course_lecturer values ($1, $3), ($1, $4), ($2, $3), ($2, $5)`,
		labOnly, mixed, lab1, lab2, regular); err != nil {
		t.Fatal(err)
	}

	get := func(t *testing.T) []CourseLecturerViolation {
		t.Helper()
		resp, err := http.Get(ts.URL + "/api/v1/course-lecturer-violations")
		if err != nil {
			t.Fatalf("Failed to send GET: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}
		var violations []CourseLecturerViolation
		if err := json.NewDecoder(resp.Body).Decode(&violations); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}
		return violations
	}

	t.Run("OnlyLabEngineerCoursesAreReported", func(t *testing.T) {
		violations := get(t)
		want := []CourseLecturerViolation{{CourseID: labOnly, Number: "C1", Name: "Nur Labor", LabEngineerCount: 2}}
		if len(violations) != 1 || violations[0] != want[0] {
			t.Errorf("Expected %v, got %v", want, violations)
		}
	})

	send := func(t *testing.T, method, path string, courseID int) int {
		t.Helper()
		body := fmt.Sprintf(`{"course_id": %d}`, courseID)
		req, _ := http.NewRequest(method, ts.URL+"/api/v1/course-lecturer-violations"+path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to send %s: %v", method, err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	getIgnored := func(t *testing.T) []IgnoredCourseLecturerViolation {
		t.Helper()
		resp, err := http.Get(ts.URL + "/api/v1/course-lecturer-violations/ignored")
		if err != nil {
			t.Fatalf("Failed to send GET: %v", err)
		}
		defer resp.Body.Close()
		var ignored []IgnoredCourseLecturerViolation
		if err := json.NewDecoder(resp.Body).Decode(&ignored); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}
		return ignored
	}

	t.Run("IgnoreHidesViolation", func(t *testing.T) {
		if status := send(t, http.MethodPost, "/ignore", labOnly); status != http.StatusCreated {
			t.Fatalf("Expected 201, got %d", status)
		}
		if status := send(t, http.MethodPost, "/ignore", labOnly); status != http.StatusConflict {
			t.Errorf("Expected 409 for a second ignore, got %d", status)
		}
		if status := send(t, http.MethodPost, "/ignore", 999999); status != http.StatusNotFound {
			t.Errorf("Expected 404 for a missing course, got %d", status)
		}
		if violations := get(t); len(violations) != 0 {
			t.Errorf("Ignored course must not be reported, got %v", violations)
		}
		if ignored := getIgnored(t); len(ignored) != 1 || ignored[0].CourseID != labOnly {
			t.Errorf("Expected the course as ignored, got %v", ignored)
		}
	})

	t.Run("UnignoreShowsViolationAgain", func(t *testing.T) {
		if status := send(t, http.MethodDelete, "/ignore", labOnly); status != http.StatusOK {
			t.Fatalf("Expected 200, got %d", status)
		}
		if status := send(t, http.MethodDelete, "/ignore", labOnly); status != http.StatusNotFound {
			t.Errorf("Expected 404 for a second unignore, got %d", status)
		}
		if violations := get(t); len(violations) != 1 {
			t.Errorf("Expected the course again, got %v", violations)
		}
	})

	t.Run("AssigningRegularLecturerSolvesViolation", func(t *testing.T) {
		if status := send(t, http.MethodPost, "/ignore", labOnly); status != http.StatusCreated {
			t.Fatalf("Expected 201, got %d", status)
		}
		if _, err := pool.Exec(ctx, `insert into course_lecturer values ($1, $2)`, labOnly, regular); err != nil {
			t.Fatal(err)
		}
		if violations := get(t); len(violations) != 0 {
			t.Errorf("Expected an empty array, got %v", violations)
		}
		// solved ignores are removed
		if ignored := getIgnored(t); len(ignored) != 0 {
			t.Errorf("Expected no ignored courses, got %v", ignored)
		}
	})

	t.Run("DeletingCourseDeletesIgnore", func(t *testing.T) {
		var course int
		mustScan(&course, `insert into course (number, name, abbreviation) values ('C4', 'Weg', 'W') returning id`)
		if _, err := pool.Exec(ctx, `insert into ignored_course_lecturer_violation values ($1)`, course); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `delete from course where id = $1`, course); err != nil {
			t.Fatalf("Course with ignore must be deletable: %v", err)
		}
	})
}
