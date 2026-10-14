package apiserver

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
)

// writeJSON writes object v into ResponseWriter w encoded as json and sets correct http headers
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// logRequest allows for manual logging of msg with the http method and path provided by r
func logRequest(r *http.Request, msg string) {
	log.Printf("[%s %s] %s", r.Method, r.URL.Path, msg)
}

// logRequest allows for manual logging of err with the http method and path provided by r
func logError(r *http.Request, err error) {
	log.Printf("[%s %s] %s", r.Method, r.URL.Path, err)
}

// parseID parses a string as an integer and validates it fits in a PostgreSQL integer (int32).
func parseID(s string) (int, error) {
	id, err := strconv.Atoi(s)
	if err != nil {
		return 0, err
	}
	if id < math.MinInt32 || id > math.MaxInt32 {
		return 0, fmt.Errorf("id %d out of int32 range", id)
	}
	return id, nil
}

// toNullableText converts a string to a nullable pgtype.Text
func toNullableText(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{Valid: false}
	}
	return pgtype.Text{String: s, Valid: true}
}

// toNullableInt4 converts a string to a nullable pgtype.Int4
func toNullableInt4(s string) (pgtype.Int4, error) {
	if s == "" {
		return pgtype.Int4{Valid: false}, nil
	}
	n, err := strconv.ParseInt(s, 10, 32)
	if err != nil {
		return pgtype.Int4{}, err
	}
	return pgtype.Int4{Int32: int32(n), Valid: true}, nil
}

// toNullableFloat8 converts a string to a nullable pgtype.Float8
func toNullableFloat8(s string) (pgtype.Float8, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return pgtype.Float8{Valid: false}, nil
	}
	s = strings.ReplaceAll(s, ",", ".")
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return pgtype.Float8{}, err
	}
	return pgtype.Float8{Float64: n, Valid: true}, nil
}
