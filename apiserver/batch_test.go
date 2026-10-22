package apiserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func postBatch(t *testing.T, url string, body string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Post(url+"/api/v1/batch", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("Failed to send POST /batch: %v", err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// result returns field ("before"/"after") of the i-th result of a batch response.
func result(out map[string]any, i int, field string) map[string]any {
	v, _ := out["results"].([]any)[i].(map[string]any)[field].(map[string]any)
	return v
}

func TestBatchEndpoint(t *testing.T) {
	pool := setupTestDB(t)
	defer pool.Close()

	server := New(pool, true)
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	var courseID, roomID int
	if err := pool.QueryRow(t.Context(),
		`insert into course (number, name, sws, credit_points, abbreviation) values ('C1', 'Batch', 2, 3, 'B') returning id`,
	).Scan(&courseID); err != nil {
		t.Fatalf("Failed to create course: %v", err)
	}
	if err := pool.QueryRow(t.Context(),
		`insert into room (number, name, seats) values ('R1', 'Raum', 10) returning id`,
	).Scan(&roomID); err != nil {
		t.Fatalf("Failed to create room: %v", err)
	}

	countRooms := func(slot int) int {
		var n int
		_ = pool.QueryRow(t.Context(), `select count(*) from time_slot_room_assignment where time_slot = $1`, slot).Scan(&n)
		return n
	}

	var slotID int

	t.Run("CreateSlotAndAssignRoom", func(t *testing.T) {
		status, out := postBatch(t, ts.URL, fmt.Sprintf(`{"ops": [
			{"kind": "create", "resource": "courseTimeSlot", "data": {"course": %d, "weekday": 1, "at": "08:00", "duration": "01:30"}}
		]}`, courseID))
		if status != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %v", status, out)
		}
		slotID = int(result(out, 0, "after")["id"].(float64))

		status, out = postBatch(t, ts.URL, fmt.Sprintf(`{"ops": [
			{"kind": "create", "resource": "timeSlotRoomAssignment", "data": {"time_slot": %d, "room": %d}}
		]}`, slotID, roomID))
		if status != http.StatusOK || countRooms(slotID) != 1 {
			t.Fatalf("Room assignment failed: %d %v", status, out)
		}
	})

	t.Run("UpdateReturnsBeforeAndAfter", func(t *testing.T) {
		status, out := postBatch(t, ts.URL, fmt.Sprintf(`{"ops": [
			{"kind": "update", "resource": "courseTimeSlot", "id": %d, "set": {"at": "10:00"}, "expect": {"at": "08:00"}}
		]}`, slotID))
		if status != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %v", status, out)
		}
		if at := result(out, 0, "before")["at"]; at != "08:00:00" {
			t.Errorf("Expected before.at 08:00:00, got %v", at)
		}
		if at := result(out, 0, "after")["at"]; at != "10:00:00" {
			t.Errorf("Expected after.at 10:00:00, got %v", at)
		}
	})

	t.Run("StaleExpectIsRejectedWith409", func(t *testing.T) {
		status, out := postBatch(t, ts.URL, fmt.Sprintf(`{"ops": [
			{"kind": "update", "resource": "courseTimeSlot", "id": %d, "set": {"at": "12:00"}, "expect": {"at": "08:00"}}
		]}`, slotID))
		if status != http.StatusConflict {
			t.Fatalf("Expected 409, got %d: %v", status, out)
		}
	})

	t.Run("ExpectOnOtherFieldDoesNotConflict", func(t *testing.T) {
		status, out := postBatch(t, ts.URL, fmt.Sprintf(`{"ops": [
			{"kind": "update", "resource": "courseTimeSlot", "id": %d, "set": {"duration": "02:00"}, "expect": {"duration": "90 minutes"}}
		]}`, slotID))
		if status != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %v", status, out)
		}
	})

	t.Run("BatchIsAtomic", func(t *testing.T) {
		// first op would succeed, second conflicts -> nothing may be applied
		status, out := postBatch(t, ts.URL, fmt.Sprintf(`{"ops": [
			{"kind": "delete", "resource": "timeSlotRoomAssignment", "data": {"time_slot": %d, "room": %d}},
			{"kind": "delete", "resource": "courseTimeSlot", "id": %d, "expect": {"at": "08:00"}}
		]}`, slotID, roomID, slotID))
		if status != http.StatusConflict {
			t.Fatalf("Expected 409, got %d: %v", status, out)
		}
		if op := out["op"]; op != 1.0 {
			t.Errorf("Expected failing op 1, got %v", op)
		}
		if countRooms(slotID) != 1 {
			t.Errorf("Room assignment was deleted although the batch failed")
		}
	})

	t.Run("DeleteSlotWithRooms", func(t *testing.T) {
		status, out := postBatch(t, ts.URL, fmt.Sprintf(`{"ops": [
			{"kind": "delete", "resource": "timeSlotRoomAssignment", "data": {"time_slot": %d, "room": %d}},
			{"kind": "delete", "resource": "courseTimeSlot", "id": %d, "expect": {"at": "10:00"}}
		]}`, slotID, roomID, slotID))
		if status != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %v", status, out)
		}
	})

	t.Run("DeletedSlotGives404", func(t *testing.T) {
		status, _ := postBatch(t, ts.URL, fmt.Sprintf(`{"ops": [
			{"kind": "update", "resource": "courseTimeSlot", "id": %d, "set": {"at": "09:00"}}
		]}`, slotID))
		if status != http.StatusNotFound {
			t.Errorf("Expected 404, got %d", status)
		}
	})

	t.Run("RestoreUnderOldID", func(t *testing.T) {
		// undo of the delete: slot and room come back, the slot keeps its id
		status, out := postBatch(t, ts.URL, fmt.Sprintf(`{"ops": [
			{"kind": "create", "resource": "courseTimeSlot", "id": %d, "data": {"course": %d, "weekday": 1, "at": "10:00", "duration": "02:00"}},
			{"kind": "create", "resource": "timeSlotRoomAssignment", "data": {"time_slot": %d, "room": %d}}
		]}`, slotID, courseID, slotID, roomID))
		if status != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %v", status, out)
		}
		if id := int(result(out, 0, "after")["id"].(float64)); id != slotID {
			t.Errorf("Expected restored id %d, got %d", slotID, id)
		}
		if countRooms(slotID) != 1 {
			t.Errorf("Room not restored")
		}
	})

	t.Run("RestoreExistingIDConflicts", func(t *testing.T) {
		status, _ := postBatch(t, ts.URL, fmt.Sprintf(`{"ops": [
			{"kind": "create", "resource": "courseTimeSlot", "id": %d, "data": {"course": %d, "weekday": 1, "at": "10:00", "duration": "02:00"}}
		]}`, slotID, courseID))
		if status != http.StatusConflict {
			t.Errorf("Expected 409, got %d", status)
		}
	})

	t.Run("RestoreNeverIssuedIDIsRejected", func(t *testing.T) {
		// would collide with a future regular insert
		status, _ := postBatch(t, ts.URL, fmt.Sprintf(`{"ops": [
			{"kind": "create", "resource": "courseTimeSlot", "id": 999999, "data": {"course": %d, "weekday": 1, "at": "10:00", "duration": "02:00"}}
		]}`, courseID))
		if status != http.StatusBadRequest {
			t.Errorf("Expected 400, got %d", status)
		}
		// the next regular insert still works and gets a fresh id
		status, out := postBatch(t, ts.URL, fmt.Sprintf(`{"ops": [
			{"kind": "create", "resource": "courseTimeSlot", "data": {"course": %d, "weekday": 2, "at": "10:00", "duration": "01:00"}}
		]}`, courseID))
		if status != http.StatusOK || int(result(out, 0, "after")["id"].(float64)) <= slotID {
			t.Errorf("Regular insert after restore failed: %d %v", status, out)
		}
	})

	t.Run("InvalidRequests", func(t *testing.T) {
		for _, body := range []string{
			`{"ops": []}`,
			`{"ops": [{"kind": "update", "resource": "room", "id": 1, "set": {}}]}`,
			`{"ops": [{"kind": "update", "resource": "courseTimeSlot", "set": {"at": "09:00"}}]}`,
			fmt.Sprintf(`{"ops": [{"kind": "create", "resource": "courseTimeSlot", "data": {"course": %d, "weekday": 9, "at": "08:00", "duration": "01:00"}}]}`, courseID),
			fmt.Sprintf(`{"ops": [{"kind": "create", "resource": "courseTimeSlot", "data": {"course": %d, "id": 1, "weekday": 1, "at": "08:00", "duration": "01:00"}}]}`, courseID),
		} {
			if status, out := postBatch(t, ts.URL, body); status != http.StatusBadRequest {
				t.Errorf("Expected 400 for %s, got %d: %v", body, status, out)
			}
		}
	})
}

func TestBatchCourseColor(t *testing.T) {
	pool := setupTestDB(t)
	defer pool.Close()

	server := New(pool, true)
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	var courseID int
	if err := pool.QueryRow(t.Context(),
		`insert into course (number, name, abbreviation) values ('C2', 'Farbe', 'F') returning id`,
	).Scan(&courseID); err != nil {
		t.Fatalf("Failed to create course: %v", err)
	}

	var blue, red int

	t.Run("CreateColorsAndAssign", func(t *testing.T) {
		status, out := postBatch(t, ts.URL, `{"ops": [
			{"kind": "create", "resource": "courseColor", "data": {"name": "Blau", "hex": "#0000FF"}},
			{"kind": "create", "resource": "courseColor", "data": {"name": "Rot", "hex": "#FF0000"}}
		]}`)
		if status != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %v", status, out)
		}
		blue = int(result(out, 0, "after")["id"].(float64))
		red = int(result(out, 1, "after")["id"].(float64))

		status, out = postBatch(t, ts.URL, fmt.Sprintf(`{"ops": [
			{"kind": "create", "resource": "courseColorAssignment", "data": {"course": %d, "color": %d}}
		]}`, courseID, blue))
		if status != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %v", status, out)
		}
	})

	t.Run("SwapAssignmentAtomically", func(t *testing.T) {
		status, out := postBatch(t, ts.URL, fmt.Sprintf(`{"ops": [
			{"kind": "delete", "resource": "courseColorAssignment", "data": {"course": %d, "color": %d}},
			{"kind": "create", "resource": "courseColorAssignment", "data": {"course": %d, "color": %d}}
		]}`, courseID, blue, courseID, red))
		if status != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %v", status, out)
		}
		// deleting the old assignment again must conflict (it is gone)
		status, _ = postBatch(t, ts.URL, fmt.Sprintf(`{"ops": [
			{"kind": "delete", "resource": "courseColorAssignment", "data": {"course": %d, "color": %d}}
		]}`, courseID, blue))
		if status != http.StatusConflict {
			t.Errorf("Expected 409, got %d", status)
		}
	})

	t.Run("RenameWithExpect", func(t *testing.T) {
		status, out := postBatch(t, ts.URL, fmt.Sprintf(`{"ops": [
			{"kind": "update", "resource": "courseColor", "id": %d, "set": {"name": "Himmelblau"}, "expect": {"name": "Blau"}}
		]}`, blue))
		if status != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %v", status, out)
		}
		if name := result(out, 0, "before")["name"]; name != "Blau" {
			t.Errorf("Expected before.name Blau, got %v", name)
		}
		// hex comparison is case-insensitive
		status, _ = postBatch(t, ts.URL, fmt.Sprintf(`{"ops": [
			{"kind": "update", "resource": "courseColor", "id": %d, "set": {"hex": "#00FF00"}, "expect": {"hex": "#0000ff"}}
		]}`, blue))
		if status != http.StatusOK {
			t.Errorf("Expected 200, got %d", status)
		}
	})

	t.Run("StaleExpectIsRejected", func(t *testing.T) {
		status, _ := postBatch(t, ts.URL, fmt.Sprintf(`{"ops": [
			{"kind": "update", "resource": "courseColor", "id": %d, "set": {"name": "X"}, "expect": {"name": "Blau"}}
		]}`, blue))
		if status != http.StatusConflict {
			t.Errorf("Expected 409, got %d", status)
		}
	})

	t.Run("DeleteColorInUseConflicts", func(t *testing.T) {
		status, _ := postBatch(t, ts.URL, fmt.Sprintf(`{"ops": [
			{"kind": "delete", "resource": "courseColor", "id": %d}
		]}`, red))
		if status != http.StatusConflict {
			t.Errorf("Expected 409, got %d", status)
		}
	})

	t.Run("DeleteAndRestoreUnderOldID", func(t *testing.T) {
		status, out := postBatch(t, ts.URL, fmt.Sprintf(`{"ops": [{"kind": "delete", "resource": "courseColor", "id": %d}]}`, blue))
		if status != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %v", status, out)
		}
		status, out = postBatch(t, ts.URL, fmt.Sprintf(`{"ops": [
			{"kind": "create", "resource": "courseColor", "id": %d, "data": {"name": "Himmelblau", "hex": "#00FF00"}}
		]}`, blue))
		if status != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %v", status, out)
		}
		if id := int(result(out, 0, "after")["id"].(float64)); id != blue {
			t.Errorf("Expected restored id %d, got %d", blue, id)
		}
	})

	t.Run("InvalidHex", func(t *testing.T) {
		status, _ := postBatch(t, ts.URL, `{"ops": [{"kind": "create", "resource": "courseColor", "data": {"name": "Kaputt", "hex": "blau"}}]}`)
		if status != http.StatusBadRequest {
			t.Errorf("Expected 400, got %d", status)
		}
	})
}

func TestBatchIgnoredConflicts(t *testing.T) {
	pool := setupTestDB(t)
	defer pool.Close()

	server := New(pool, true)
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	ctx := t.Context()
	var courseID, roomID, s1, s2, lecturerID, constraintID int
	mustScan := func(dest *int, query string, args ...any) {
		t.Helper()
		if err := pool.QueryRow(ctx, query, args...).Scan(dest); err != nil {
			t.Fatalf("setup %q: %v", query, err)
		}
	}
	mustScan(&courseID, `insert into course (number, name, abbreviation) values ('C3', 'Ignore', 'I') returning id`)
	mustScan(&roomID, `insert into room (number) values ('R3') returning id`)
	mustScan(&s1, `insert into course_time_slots (course, weekday, at, duration) values ($1, 1, '08:00', '01:30') returning id`, courseID)
	mustScan(&s2, `insert into course_time_slots (course, weekday, at, duration) values ($1, 1, '08:00', '01:30') returning id`, courseID)
	mustScan(&lecturerID, `insert into lecturer (first_name, last_name, abbreviation, kind) values ('A', 'B', 'AB', 'regular') returning id`)
	mustScan(&constraintID, `insert into lecturer_negative_time_constraint (lecturer, weekday, at, duration, soft) values ($1, 1, '08:00', '02:00', false) returning id`, lecturerID)
	if _, err := pool.Exec(ctx, `insert into ignored_resource_conflict values ($1, $2, 'room', $3)`, s1, s2, roomID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into ignored_lecturer_constraint_violation values ($1, $2)`, constraintID, s1); err != nil {
		t.Fatal(err)
	}

	t.Run("DeleteSlotReportsIgnoresAsCascade", func(t *testing.T) {
		status, out := postBatch(t, ts.URL, fmt.Sprintf(`{"ops": [
			{"kind": "delete", "resource": "courseTimeSlot", "id": %d}
		]}`, s1))
		if status != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %v", status, out)
		}
		cascade := out["results"].([]any)[0].(map[string]any)["cascade"].([]any)
		if len(cascade) != 2 {
			t.Fatalf("Expected 2 cascade rows, got %v", cascade)
		}
		resources := []any{cascade[0].(map[string]any)["resource"], cascade[1].(map[string]any)["resource"]}
		if resources[0] != "ignoredResourceConflict" || resources[1] != "ignoredLecturerConstraintViolation" {
			t.Errorf("Unexpected cascade resources %v", resources)
		}
	})

	t.Run("RestoreSlotAndIgnoresUnderOldIDs", func(t *testing.T) {
		status, out := postBatch(t, ts.URL, fmt.Sprintf(`{"ops": [
			{"kind": "create", "resource": "courseTimeSlot", "id": %d, "data": {"course": %d, "weekday": 1, "at": "08:00", "duration": "01:30"}},
			{"kind": "create", "resource": "ignoredResourceConflict", "data": {"slot_a": %d, "slot_b": %d, "resource_type": "room", "resource_id": %d}},
			{"kind": "create", "resource": "ignoredLecturerConstraintViolation", "data": {"constraint_id": %d, "time_slot_id": %d}},
			{"kind": "create", "resource": "ignoredResourceConflict", "data": {"slot_a": 999999, "slot_b": %d, "resource_type": "room", "resource_id": %d}}
		]}`, s1, courseID, s1, s2, roomID, constraintID, s1, s2, roomID))
		if status != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %v", status, out)
		}
		var a, b int
		if err := pool.QueryRow(ctx, `select slot_a, slot_b from ignored_resource_conflict where resource_id = $1`, roomID).Scan(&a, &b); err != nil {
			t.Fatalf("Ignore not restored: %v", err)
		}
		if a != s1 || b != s2 {
			t.Errorf("Expected the original pair (%d, %d), got (%d, %d)", s1, s2, a, b)
		}
		var n int
		_ = pool.QueryRow(ctx, `select count(*) from ignored_lecturer_constraint_violation where time_slot_id = $1`, s1).Scan(&n)
		if n != 1 {
			t.Errorf("Lecturer ignore not restored")
		}
		if skipped := result(out, 3, "after"); skipped != nil {
			t.Errorf("Marker for missing slot should be skipped, got %v", skipped)
		}
	})

	t.Run("DeletingMissingMarkerIsNoError", func(t *testing.T) {
		status, out := postBatch(t, ts.URL, fmt.Sprintf(`{"ops": [
			{"kind": "delete", "resource": "ignoredLecturerConstraintViolation", "data": {"constraint_id": %d, "time_slot_id": 999999}}
		]}`, constraintID))
		if status != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %v", status, out)
		}
	})
}
