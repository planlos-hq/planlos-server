package apiserver

// import_export_csv.go handles the CSV and excel (xlsx) import and export.
// Both /import endpoints call the same handler and auto-detect the content type (this is kept for backwards compatibility).
// The ressource type is also auto-detected by column headings.
// XLSX files get converted to CSV and use the same insertion logic.
// Empty columns and rows (from the left / top), trailing empty cells,
// as well as auto generated excel columns (Column 1, Column 2, ...) get skipped

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/xuri/excelize/v2"
)

func registerCSVRoutes(s *Server, mux *http.ServeMux) {
	mux.HandleFunc("POST /import/csv", s.postCSVImport)
	mux.HandleFunc("POST /import/xlsx", s.postCSVImport)
	mux.HandleFunc("GET /export/csv/{resource}", s.postCSVExport)
	mux.HandleFunc("GET /export/xlsx/{resource}", s.postXLSXExport)
}

type CSVErrorResponse struct {
	LineNr  int    `json:"line_nr"`  // Line number where the error occured (only where applicable, otherwise 0)
	ErrType string `json:"err_type"` // XLSX_TO_CSV, INVALID_CONTENT_TYPE, INVALID_HEADER_NAME, INVALID_REFERENCE, UNIQUE, CHECK, REQUIRED_FIELD_EMPTY, INVALID_FIELD_VALUE, INVALID_JSON, OTHER
	Message string `json:"message"`
}

type CSVSuccessResponse struct {
	ImportedEntries int `json:"imported_entries"` // Number of imported entries
}

// Handler: POST /import/csv,
// POST /import/xlsx;
// Import resources from an uploaded CSV or XLSX file (format is auto-detected)
func (s *Server) postCSVImport(w http.ResponseWriter, r *http.Request) {
	const maxUploadSize = 15 << 20 // Max 15 MB
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)

	reader, err := r.MultipartReader()
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		logRequest(r, "Not a Multipart Request")
		logError(r, err)
		return
	}

	part, err := reader.NextPart()
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		logError(r, err)
		return
	}
	defer part.Close()

	// Detect content (csv or xslx)
	sniffBuf := make([]byte, 512)
	n, err := io.ReadFull(part, sniffBuf)
	if err != nil && err != io.ErrUnexpectedEOF {
		w.WriteHeader(http.StatusInternalServerError)
		logError(r, err)
		return
	}
	contentType := strings.Split(http.DetectContentType(sniffBuf[:n]), ";")[0]

	// Re-insert sniffed bytes into reader
	var data io.Reader = io.MultiReader(bytes.NewReader(sniffBuf[:n]), part)
	// Also validate file extension for xslx (might be an older file format)
	fileExt := strings.ToLower(filepath.Ext(part.FileName()))
	if contentType == "application/zip" && fileExt == ".xlsx" {
		data, err = xslxToCSV(data)
		if err != nil {
			resp := CSVErrorResponse{
				ErrType: "XLSX_TO_CSV",
				Message: err.Error(),
			}
			writeJSON(w, http.StatusUnprocessableEntity, resp)
			return
		}
	} else if contentType != "text/plain" && contentType != "text/csv" {
		resp := CSVErrorResponse{
			ErrType: "INVALID_CONTENT_TYPE",
			Message: "Only CSV and XLSX files are allowed",
		}
		writeJSON(w, http.StatusBadRequest, resp)
		return
	}

	lines, err := s.importCSV(r.Context(), data)
	if err != nil {
		logError(r, err)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			errType := "OTHER"
			switch pgErr.Code {
			case "23505": // Unique violation (not sure if this can happen)
				errType = "UNIQUE"
			case "23502": // NOT NULL (triggered when foreign key not found)
				errType = "INVALID_REFERENCE"
			case "23514": // Check constraint violation
				errType = "CHECK"
			}
			resp := CSVErrorResponse{
				LineNr:  lines,
				ErrType: errType,
				Message: pgErr.Message,
			}
			writeJSON(w, http.StatusUnprocessableEntity, resp)
			return
		}
		errType := "OTHER"
		if errors.Is(err, errRequiredFieldEmpty) {
			errType = "REQUIRED_FIELD_EMPTY"
		} else if errors.Is(err, errInvalidFieldValue) {
			errType = "INVALID_FIELD_VALUE"
		} else if errors.Is(err, errResTypeNotFound) {
			errType = "INVALID_HEADER_NAME"
		} else if errors.Is(err, errInvalidJSON) {
			errType = "INVALID_JSON"
		}
		resp := CSVErrorResponse{
			LineNr:  lines,
			ErrType: errType,
			Message: err.Error(),
		}
		writeJSON(w, http.StatusUnprocessableEntity, resp)
		return
	}

	success := CSVSuccessResponse{lines}
	writeJSON(w, http.StatusOK, success)
}

type resourceType string

// All importable ressources
const (
	rtLecturer      resourceType = "lecturer"
	rtRoom          resourceType = "room"
	rtDegreeProgram resourceType = "degree_program"
	rtStudyGroup    resourceType = "study_group"
	rtCourse        resourceType = "course"
)

// CSV columns for each resource type
var rtFieldCounts = map[resourceType][]string{
	rtLecturer:      {"titel", "vorname", "nachname", "kürzel", "art", "notizen", "extra_daten"},
	rtRoom:          {"nummer", "name", "plätze", "extra_daten"},
	rtDegreeProgram: {"name", "kürzel", "extra_daten"},
	rtStudyGroup:    {"saison", "jahr", "studenten_anzahl", "studiengang", "extra_daten"},
	rtCourse:        {"nummer", "name", "sws", "credit_points", "kürzel", "extra_daten"},
}

type lecturerKind string

const (
	lkRegular          string = "regular"
	lkAdjunctProfessor string = "adjunct_professor"
	lkLabEngineer      string = "lab_engineer"
)

type season string

const (
	sSummer season = "summer"
	sWinter season = "winter"
)

// matchSeason matches different notations to a season
func matchSeason(seasonStr string) (season, error) {
	seasonLower := strings.ToLower(strings.TrimSpace(seasonStr))
	switch {
	case slices.Contains([]string{"summer", "sommer", "s"}, seasonLower):
		return sSummer, nil
	case slices.Contains([]string{"winter", "w"}, seasonLower):
		return sWinter, nil
	default:
		return "", fmt.Errorf("unknown season: %s, %w", seasonStr, errInvalidFieldValue)
	}
}

// matchLecturerKind matches different notations to a lecturer kind
func matchLecturerKind(kind string) (lecturerKind, error) {
	kindLower := strings.ToLower(strings.TrimSpace(kind))
	switch {
	case slices.Contains([]string{"regular", "regulär"}, kindLower):
		return lecturerKind(lkRegular), nil
	case slices.Contains([]string{"adjunct_professor", "adjunct professor", "lehrbeauftragter"}, kindLower):
		return lecturerKind(lkAdjunctProfessor), nil
	case slices.Contains([]string{"lab engineer", "lab_engineer", "laboringenieur"}, kindLower):
		return lecturerKind(lkLabEngineer), nil
	default:
		return "", fmt.Errorf("unknown lecturer kind: %s, %w", kind, errInvalidFieldValue)
	}
}

var errResTypeNotFound = errors.New("no matching resource type found, header names invalid")

//go:embed queries/create_study_group_from_dp_name.sql
var createStudyGroupFromDPName string

// importCSV imports CSV data from r and returns the number of entries imported.
// If a database error occurs, the offending line number is returned along with the pgconn.PgError.
func (s *Server) importCSV(ctx context.Context, r io.Reader) (int, error) {
	reader := csv.NewReader(r)
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = 0

	header, err := reader.Read()
	if err != nil {
		return 0, fmt.Errorf("read csv header %w", err)
	}

	// Determine resource type from header fields
	var resType resourceType
	for res, fieldNames := range rtFieldCounts {
		// Allow more header fields than expected (thanks, Excel!)
		if len(fieldNames) > len(header) {
			continue
		}
		match := true
		for i, field := range header {
			if i >= len(fieldNames) {
				break
			}
			// Strip UTF8 byte order marker
			if strings.EqualFold(strings.TrimSpace(strings.TrimPrefix(field, "\ufeff")), fieldNames[i]) {
				continue
			}
			//fmt.Printf("%q does not equal %q\n", field, fieldNames[i])
			match = false
			break
		}
		if match {
			resType = res
			break
		}
	}

	if resType == "" {
		return 0, errResTypeNotFound
	}

	// Disable field count validation to enable support for bad office software
	reader.FieldsPerRecord = -1

	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	batch := &pgx.Batch{}

	// Iterate through all rows, create the insert statements and add them to the batch.
	// Batch line nr = CSV line nr - 1 (header)
	lineNum := 1
	emptyCounter := 0
	for {
		lineNum++
		row, err := reader.Read()

		if err == io.EOF {
			break
		}

		if err != nil {
			return lineNum, fmt.Errorf("read csv row at line %d: %w", lineNum, err)
		}

		// Skip entirely empty rows (yes, it's Excels fault)
		empty := true
		for _, str := range row {
			if strings.TrimSpace(str) != "" {
				empty = false
			}
		}
		if empty {
			emptyCounter++
			continue
		}

		// Validate the required fields for this row
		if err := validateCSVRow(row, resType); err != nil {
			return lineNum, fmt.Errorf("validate csv row at line %d: %w", lineNum, err)
		}

		// Parse the row and create the insert statement.
		// No data validation needed, as this is already done in validateCSVRow.
		// Empty strings get converted to null values.
		switch resType {
		case rtLecturer:
			kind, _ := matchLecturerKind(row[4]) //nolint:errcheck
			batch.Queue(createLecturerQuery, row[1], row[2], row[3], kind, toNullableText(row[5]), true, extraDataOrDefault(row[6]), toNullableText(row[0]))
		case rtRoom:
			seats, err := toNullableInt4(row[2])
			if err != nil {
				return lineNum, fmt.Errorf("parse seats at line %d: %w", lineNum, err)
			}
			batch.Queue(createRoomQuery, row[0], toNullableText(row[1]), seats, extraDataOrDefault(row[3]))
		case rtDegreeProgram:
			batch.Queue(createDegreeProgramQuery, row[0], toNullableText(row[1]), extraDataOrDefault(row[2]))
		case rtStudyGroup:
			season, _ := matchSeason(row[0])
			year, err := toNullableInt4(row[1])
			if err != nil || year.Valid == false {
				return lineNum, fmt.Errorf("parse year at line %d: %w", lineNum, err)
			}
			studentCount, err := toNullableInt4(row[2])
			if err != nil || studentCount.Valid == false {
				return lineNum, fmt.Errorf("parse student count at line %d: %w", lineNum, err)
			}
			batch.Queue(createStudyGroupFromDPName, season, year, studentCount, row[3], extraDataOrDefault(row[4]))
		case rtCourse:
			sws, err := toNullableFloat8(row[2])
			if err != nil {
				return lineNum, fmt.Errorf("parse sws at line %d: %w", lineNum, err)
			}
			creditPoints, err := toNullableFloat8(row[3])
			if err != nil {
				return lineNum, fmt.Errorf("parse credit points at line %d: %w", lineNum, err)
			}
			batch.Queue(createCourseQuery, row[0], row[1], sws, creditPoints, row[4], extraDataOrDefault(row[5]))
		}
	}

	br := tx.SendBatch(ctx, batch)

	// Execute batch line by line
	for i := range batch.Len() {
		if _, err := br.Exec(); err != nil {
			_ = br.Close()
			return i + 2, fmt.Errorf("execute batch index %d: %w", i, err) // Rollback deferred
		}
	}

	if err := br.Close(); err != nil {
		return 0, fmt.Errorf("close batch: %w", err)
	}

	// Commit only if all queries are successful
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit transaction: %w", err)
	}

	return lineNum - emptyCounter - 2, nil
}

// extraDataOrDefault returns an empty JSON object if empty
func extraDataOrDefault(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "{}"
	}
	return s
}

var errRequiredFieldEmpty = errors.New("required field empty")
var errInvalidFieldValue = errors.New("invalid field value")
var errInvalidJSON = errors.New("invalid json")

func validateCSVRow(row []string, resType resourceType) error {
	// Length of each row not guaranteed
	if len(row) < len(rtFieldCounts[resType]) {
		return fmt.Errorf("invalid row length: expected at least %d, got %d", len(rtFieldCounts[resType]), len(row))
	}

	switch resType {
	case rtLecturer:
		if row[1] == "" || row[2] == "" || row[3] == "" || row[4] == "" {
			return errRequiredFieldEmpty
		}
		if _, err := matchLecturerKind(row[4]); err != nil {
			return err
		}
	case rtRoom:
		if row[0] == "" {
			return fmt.Errorf("required field 'number' empty: %w", errRequiredFieldEmpty)
		}
		if row[2] != "" {
			if seats, err := strconv.Atoi(row[2]); err != nil {
				return fmt.Errorf("number of seats not numeric: %w", errInvalidFieldValue)
			} else if seats < 0 {
				return fmt.Errorf("number of seats must be positive: %w", errInvalidFieldValue)
			}
		}
	case rtDegreeProgram:
		if row[0] == "" || row[1] == "" {
			return errRequiredFieldEmpty
		}
	case rtStudyGroup:
		if row[0] == "" || row[1] == "" || row[2] == "" || row[3] == "" {
			return errRequiredFieldEmpty
		}
		if _, err := matchSeason(row[0]); err != nil {
			return err
		}
		if year, err := strconv.Atoi(row[1]); err != nil {
			return fmt.Errorf("year not numeric: %w", errInvalidFieldValue)
		} else if year < 2000 {
			return fmt.Errorf("year must be greater than 2000: %w", errInvalidFieldValue)
		}
		if studentCount, err := strconv.Atoi(row[2]); err != nil {
			return fmt.Errorf("student count not numeric: %w", errInvalidFieldValue)
		} else if studentCount < 0 {
			return fmt.Errorf("student count must be positive: %w", errInvalidFieldValue)
		}
	case rtCourse:
		if row[0] == "" || row[1] == "" || row[4] == "" {
			return errRequiredFieldEmpty
		}
		if row[2] != "" {
			if sws, err := toNullableFloat8(row[2]); err != nil {
				return fmt.Errorf("sws not numeric: %w", errInvalidFieldValue)
			} else if sws.Valid && sws.Float64 < 0 {
				return fmt.Errorf("sws must be positive: %w", errInvalidFieldValue)
			}
		}
		if row[3] != "" {
			if cp, err := toNullableFloat8(row[3]); err != nil {
				return fmt.Errorf("credit points not numeric: %w", errInvalidFieldValue)
			} else if cp.Valid && cp.Float64 < 0 {
				return fmt.Errorf("credit points must be positive: %w", errInvalidFieldValue)
			}
		}
	}

	extraDataIdx := len(rtFieldCounts[resType]) - 1
	if v := strings.TrimSpace(row[extraDataIdx]); v != "" && !json.Valid([]byte(v)) {
		return fmt.Errorf("extra_daten is not valid JSON: %w", errInvalidJSON)
	}

	return nil
}

//go:embed queries/copy_lecturer_csv.sql
var copyLecturerQuery string

//go:embed queries/copy_room_csv.sql
var copyRoomQuery string

//go:embed queries/copy_degree_program_csv.sql
var copyDegreeProgramQuery string

//go:embed queries/copy_study_group_csv.sql
var copyStudyGroupQuery string

//go:embed queries/copy_course_csv.sql
var copyCourseQuery string

func copyQueryForResource(resource string) (string, bool) {
	queries := map[resourceType]string{
		rtLecturer:      copyLecturerQuery,
		rtRoom:          copyRoomQuery,
		rtDegreeProgram: copyDegreeProgramQuery,
		rtStudyGroup:    copyStudyGroupQuery,
		rtCourse:        copyCourseQuery,
	}
	q, ok := queries[resourceType(resource)]
	return q, ok
}

// Handler: GET /export/csv/{resource}; Export a single resource type as CSV
func (s *Server) postCSVExport(w http.ResponseWriter, r *http.Request) {
	query, ok := copyQueryForResource(r.PathValue("resource"))
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	conn, err := s.DB.Acquire(ctx)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer conn.Release()

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=\"export.csv\"")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	if _, err := conn.Conn().PgConn().CopyTo(ctx, w, query); err != nil {
		// Can't really do anything here, headers already sent
		logError(r, err)
	}
}

// Handler: GET /export/xlsx/{resource}; Export a single resource type as XLSX
func (s *Server) postXLSXExport(w http.ResponseWriter, r *http.Request) {
	query, ok := copyQueryForResource(r.PathValue("resource"))
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	conn, err := s.DB.Acquire(ctx)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer conn.Release()

	var csvBuf bytes.Buffer
	if _, err := conn.Conn().PgConn().CopyTo(ctx, &csvBuf, query); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		logError(r, err)
		return
	}

	xlsxBuf, err := csvToXLSX(&csvBuf)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		logError(r, err)
		return
	}

	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", "attachment; filename=\"export.xlsx\"")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Write(xlsxBuf.Bytes())
}

func csvToXLSX(csvData *bytes.Buffer) (*bytes.Buffer, error) {
	reader := csv.NewReader(csvData)
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read csv for xlsx conversion: %w", err)
	}

	f := excelize.NewFile()
	defer f.Close()
	sheet := "Sheet1"

	// Not sure if there is a better way to do this, but row by row works
	for rowIdx, row := range rows {
		cell, _ := excelize.CoordinatesToCellName(1, rowIdx+1)
		f.SetSheetRow(sheet, cell, &row)
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, fmt.Errorf("write xlsx: %w", err)
	}
	return &buf, nil
}

func xslxToCSV(xslxData io.Reader) (*bytes.Buffer, error) {
	f, err := excelize.OpenReader(xslxData)
	if err != nil {
		return nil, fmt.Errorf("open xslx reader: %w", err)
	}
	defer f.Close()

	// Get first sheet
	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, fmt.Errorf("no sheets found in xslx file")
	}
	sheetName := sheets[0]

	// The whole sheet needs to be read to remove empty cells
	rows, err := f.Rows(sheetName)
	if err != nil {
		return nil, fmt.Errorf("read sheet: %w", err)
	}
	defer rows.Close()

	var data [][]string
	for rows.Next() {
		row, err := rows.Columns() // Cell values for this row
		if err != nil {
			return nil, fmt.Errorf("get columns: %w", err)
		}
		data = append(data, row)
	}

	data = trimEmptyCells(data)

	// Pad all rows to the same length (otherwise trailing empty cells would be missing)
	maxCols := maxRowLength(data)
	for i, row := range data {
		if len(row) < maxCols {
			data[i] = append(row, make([]string, maxCols-len(row))...)
		}
	}

	// Cut off Excels idiotic auto generated column names
	if strings.TrimSpace(data[0][0]) == "Column1" {
		data = data[1:]
	}

	// Use a CSV writer for correct formatting + escaping
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.WriteAll(data); err != nil {
		return nil, fmt.Errorf("write csv: %w", err)
	}

	//fmt.Println(buf.String())

	return &buf, nil
}

// maxRowLength returns the length of the longest row
func maxRowLength(table [][]string) int {
	longestRow := slices.MaxFunc(table, func(a, b []string) int {
		return len(a) - len(b)
	})
	return len(longestRow)
}

// trimEmptyCells trims all fully empty columns and rows from the top and left of the array
func trimEmptyCells(table [][]string) [][]string {
	firstRow := 0
	for _, row := range table {
		if !isRowEmpty(row) {
			break
		}
		firstRow++
	}
	// Cut off empty rows from the top
	table = table[firstRow:]

	maxCols := maxRowLength(table)

	// Find first col with data
	firstCol := 0
	for i := range maxCols {
		if !isRowEmpty(extractColumn(table, i)) {
			break
		}
		firstCol++
	}

	// Cut off empty columns
	for i := range table {
		cut := min(firstCol, len(table[i]))
		table[i] = table[i][cut:]
	}

	return table
}

func extractColumn(table [][]string, index int) []string {
	col := make([]string, len(table))
	for i, row := range table {
		if len(row) <= index || index < 0 {
			// Allow shorter rows (empty vs "")
			// OOOOOOOO
			// OO <- We don't want to stop checking here
			// OOOO[DATA]
			continue
		}
		col[i] = row[index]
	}
	return col
}

func isRowEmpty(row []string) bool {
	for _, val := range row {
		if strings.TrimSpace(val) != "" {
			return false
		}
	}
	return true
}
