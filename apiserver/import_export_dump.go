package apiserver

// import_export_dump.go handles the import and export of compressed database dumps.
// Each backup is a zip file containing the database dump (.stdp) and a metadata.json file.
// The metadata includes a user comment, the schema version as well as the creation date.
// Every download sent to the user also gets saved in the container under /root/backups/ (a volume in compose.yml).
// These backups are split between development and release branches, indexed in index.json, and can be directly restored from the server.
// This file also contains the /truncate endpoint, which forces a backup before truncating the db.

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"stundenplan_api/migration"
	"sync"
	"time"
)

func registerImportExportRoutes(s *Server, mux *http.ServeMux) {
	mux.HandleFunc("GET /export/dump", s.getDumpExport)
	mux.HandleFunc("GET /backups", s.getBackups)
	mux.HandleFunc("GET /backups/{id}", s.getBackup)
	mux.HandleFunc("DELETE /backups/{id}", s.deleteBackup)
	mux.HandleFunc("POST /import/dump", s.postDumpImport)
	mux.HandleFunc("POST /backups/restore/{id}", s.postBackupRestore)
	mux.HandleFunc("POST /truncate", s.postTruncate)
}

// --- Backup Index ---

// Metadata represents the metadata of a single backup
type Metadata struct {
	SchemaVersion int    `json:"schemaVersion"`
	Comment       string `json:"comment"`
	CreatedAt     string `json:"createdAt"`
}

// IndexEntry represents a single entry in the backup index.json
type IndexEntry struct {
	Metadata

	ID       string `json:"id"`
	Filename string `json:"filename"`
}

// Index represents the index.json file in memory
type Index struct {
	Entries []IndexEntry `json:"entries"`
}

// backupIndex is a singleton instance of the backup index.
// Do not use directly, use getIndex() instead!
var (
	backupIndex *Index
	indexMu     sync.RWMutex
)

// getIndex loads the index from disk or creates a new one
func getIndex() (*Index, error) {
	// Load index from file if not already loaded
	// Write lock because backupIndex may be initialized here
	indexMu.Lock()
	defer indexMu.Unlock()
	if backupIndex == nil {
		indexPath := filepath.Join(persistentZipPath, "index.json")
		if _, err := os.Stat(indexPath); err == nil {
			// Index already exists
			data, err := os.ReadFile(indexPath)
			if err != nil {
				return nil, fmt.Errorf("read index.json: %w", err)
			}
			err = json.Unmarshal(data, &backupIndex)
			if err != nil {
				return nil, fmt.Errorf("unmarshal index.json: %w", err)
			}
		} else if os.IsNotExist(err) {
			// Index missing, create it
			backupIndex = &Index{Entries: []IndexEntry{}}
		}
	}
	return backupIndex, nil
}

// persistToDisk writes the backup index to disk.
func (idx *Index) persistToDiskUnsync() error {
	indexJSON, err := json.Marshal(idx)
	if err != nil {
		return fmt.Errorf("marshal index: %w", err)
	}
	indexPath := filepath.Join(persistentZipPath, "index.json")
	err = os.WriteFile(indexPath, indexJSON, 0600) //nolint:mnd
	if err != nil {
		return fmt.Errorf("write index.json: %w", err)
	}
	return nil
}

// Add appends a new backup to the index and writes to disk
func (idx *Index) Add(filename string, meta *Metadata) error {
	indexMu.Lock()
	defer indexMu.Unlock()
	id, err := generateID(idx)
	if err != nil {
		return fmt.Errorf("generate id: %w", err)
	}

	// Append new entry
	entry := IndexEntry{
		ID:       id,
		Filename: filename,
		Metadata: *meta,
	}
	idx.Entries = append(idx.Entries, entry)

	return idx.persistToDiskUnsync()
}

var errBackupNotFound = fmt.Errorf("backup not found")

// Remove removes a backup from the index and writes to disk
func (idx *Index) Remove(id string) (IndexEntry, error) {
	indexMu.Lock()
	defer indexMu.Unlock()
	for i, entry := range idx.Entries {
		if entry.ID == id {
			idx.Entries = append(idx.Entries[:i], idx.Entries[i+1:]...)
			return entry, idx.persistToDiskUnsync()
		}
	}
	return IndexEntry{}, errBackupNotFound
}

// Get retrieves a backup from the index by ID
func (idx *Index) Get(id string) (IndexEntry, error) {
	indexMu.RLock()
	defer indexMu.RUnlock()
	for _, entry := range idx.Entries {
		if entry.ID == id {
			return entry, nil
		}
	}
	return IndexEntry{}, errBackupNotFound
}

// generateID generates a random 16 character ID to identify backups.
// The caller must hold indexMu.
func generateID(idx *Index) (string, error) {
	for {
		b := make([]byte, 8)
		_, err := rand.Read(b)
		if err != nil {
			return "", fmt.Errorf("generate random id: %w", err)
		}
		id := hex.EncodeToString(b)
		// Retry if id already exists
		if !slices.ContainsFunc(idx.Entries, func(e IndexEntry) bool { return e.ID == id }) {
			return id, nil
		}
	}
}

// -- Import / Export Handlers ---

// Backup path in the container
const persistentZipPath = "/root/backups/"

// getDBCredentials returns the database password and a passwordless connection url (extracted from $DATABASE_URL)
func getDBCredentials() (password string, rest string, err error) {
	dbURL := os.Getenv("DATABASE_URL")
	u, err := url.Parse(dbURL)
	if err != nil {
		return "", "", fmt.Errorf("parse db url: %w", err)
	}
	pw, _ := u.User.Password()
	// Remove pw from URL
	u.User = url.User(u.User.Username())
	return pw, u.String(), nil
}

//go:embed queries/max_schema_version.sql
var getSchemaVersionQuery string

// createBackup runs pg_dump, bundles the dump and metadata.json into a zip in
// persistentZipPath and registers it in the backup index.
// Returns the path of the created zip and the backup filename.
func (s *Server) createBackup(ctx context.Context, comment string) (zipPath, filename string, err error) {
	filename = time.Now().Format("2006-01-2_15-04-05") + ".stdp"
	// Create temp directory to avoid collisions
	dir, err := os.MkdirTemp("", "stundenplan-")
	if err != nil {
		return "", "", fmt.Errorf("create temp directory: %w", err)
	}
	dumpPath := filepath.Join(dir, filename)
	defer func(path string) {
		// The packed backup will be moved to the backup directory, the temp files get deleted
		err := os.RemoveAll(path)
		if err != nil {
			log.Printf("[import_export/createBackup] Couldn't remove temp directory %s: %s", path, err)
		}
	}(dir)

	password, conURL, err := getDBCredentials()
	if err != nil {
		return "", "", fmt.Errorf("get db credentials: %w", err)
	}

	// Run pg_dump
	dumpCtx, cancelDump := context.WithTimeout(ctx, time.Minute)
	defer cancelDump()
	dumpCmd := exec.CommandContext( //nolint:gosec
		dumpCtx,
		"pg_dump",
		conURL,
		"-F",
		"c",               // Custom (binary) format
		"--compress=0",    // Will be compressed later
		"--data-only",     // Don't export schema
		"--no-owner",      // No ALTER OWNER
		"--no-privileges", // No GRANT / REVOKE
		"--exclude-table=authorized_user",
		"-f",
		dumpPath,
	)
	// Pass as env to avoid credentials from showing up in process names
	dumpCmd.Env = append(os.Environ(), "PGPASSWORD="+password)
	if err = dumpCmd.Run(); err != nil {
		return "", "", fmt.Errorf("pg_dump: %w", err)
	}

	// Get schema version
	const timeout = time.Second * 3
	queryCtx, cancelQuery := context.WithTimeout(ctx, timeout)
	defer cancelQuery()
	var schemaVersion int
	err = s.DB.QueryRow(queryCtx, getSchemaVersionQuery).Scan(&schemaVersion)
	if err != nil {
		return "", "", fmt.Errorf("get schema version: %w", err)
	}

	// Save schema version and comment (optional) in metadata.json
	metadataPath := filepath.Join(dir, "metadata.json")
	metadata := Metadata{
		SchemaVersion: schemaVersion,
		Comment:       comment,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return "", "", fmt.Errorf("marshal metadata: %w", err)
	}
	err = os.WriteFile(metadataPath, data, 0600) //nolint:mnd
	if err != nil {
		return "", "", fmt.Errorf("write metadata.json: %w", err)
	}

	// Zip compress / bundle dump and metadata.json
	err = os.MkdirAll(persistentZipPath, 0700) //nolint:mnd
	if err != nil {
		return "", "", fmt.Errorf("create backup directory: %w", err)
	}
	zipPath = filepath.Join(persistentZipPath, filename+".zip")
	zipFile, err := os.Create(zipPath) //nolint:gosec
	if err != nil {
		return "", "", fmt.Errorf("create zip file: %w", err)
	}
	zw := zip.NewWriter(zipFile)
	for _, file := range [2]string{dumpPath, metadataPath} {
		err = addToZip(zw, file)
		if err != nil {
			_ = zw.Close()
			_ = zipFile.Close()
			return "", "", fmt.Errorf("add %s to zip: %w", filepath.Base(file), err)
		}
	}

	// Close zip writer and file before serving so the central directory is written
	if err = zw.Close(); err != nil {
		_ = zipFile.Close()
		return "", "", fmt.Errorf("close zip writer: %w", err)
	}
	if err = zipFile.Close(); err != nil {
		return "", "", fmt.Errorf("close zip file: %w", err)
	}

	idx, err := getIndex()
	if err != nil {
		return "", "", fmt.Errorf("get backup index: %w", err)
	}
	err = idx.Add(filename, &metadata)
	if err != nil {
		return "", "", fmt.Errorf("add backup to index: %w", err)
	}

	return zipPath, filename, nil
}

// Handler: GET /export/dump; Create a backup of the database and send it to the client as a zip
func (s *Server) getDumpExport(w http.ResponseWriter, r *http.Request) {
	zipPath, filename, err := s.createBackup(r.Context(), r.URL.Query().Get("comment"))
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Send file
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	http.ServeFile(w, r, zipPath) //nolint:gosec
}

// Handler: POST /import/dump; Restore the database from an uploaded backup zip
func (s *Server) postDumpImport(w http.ResponseWriter, r *http.Request) {
	maintenance.Store(true)
	defer maintenance.Store(false)
	dbMu.Lock()
	defer dbMu.Unlock()

	const maxUploadSize = 15 << 20 // Max 15 MB
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)

	reader, err := r.MultipartReader()
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		logRequest(r, "Not a Multipart Request")
		logError(r, err)
		return
	}

	part, err := reader.NextPart() // Get first (and only) part
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		logError(r, err)
		return
	}
	defer func(part *multipart.Part) {
		err := part.Close()
		if err != nil {
			logError(r, err)
		}
	}(part)

	// Buffer upload to temp file (zip requires random access)
	tmpFile, err := os.CreateTemp("", "stundenplan-import-*.zip")
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer func(name string) {
		err := os.Remove(name)
		if err != nil {
			logError(r, err)
		}
	}(tmpFile.Name())
	defer tmpFile.Close()

	if _, err := io.Copy(tmpFile, part); err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	rc, metadata, dumpFile, err := extractFromZip(tmpFile.Name())
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	defer rc.Close()

	dumpReader, err := dumpFile.Open()
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer dumpReader.Close()

	if err := s.loadAndMigrate(r, dumpReader, metadata); err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// Handler: POST /backups/restore/{id}; Restore the database from a backup stored on the server
func (s *Server) postBackupRestore(w http.ResponseWriter, r *http.Request) {
	// Lock maintenance gate
	maintenance.Store(true)
	defer maintenance.Store(false)
	dbMu.Lock()
	defer dbMu.Unlock()

	id := r.PathValue("id")
	index, err := getIndex()
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	entry, err := index.Get(id)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	zipPath := filepath.Join(persistentZipPath, entry.Filename+".zip")
	rc, metadata, dumpFile, err := extractFromZip(zipPath)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer rc.Close()

	dumpReader, err := dumpFile.Open()
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer dumpReader.Close()

	if err := s.loadAndMigrate(r, dumpReader, metadata); err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// Handler: POST /truncate; Truncate all tables (except authorized_user) and send a backup of the previous state to the client.
// If the backup fails, nothing is truncated.
func (s *Server) postTruncate(w http.ResponseWriter, r *http.Request) {
	// Lock maintenance gate
	maintenance.Store(true)
	defer maintenance.Store(false)
	dbMu.Lock()
	defer dbMu.Unlock()

	comment := r.URL.Query().Get("comment")
	if comment == "" {
		comment = "Automatisches Backup vor dem Leeren der Datenbank"
	}

	// Create backup first, abort on failure
	zipPath, filename, err := s.createBackup(r.Context(), comment)
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	if _, err := s.DB.Exec(ctx, truncateEverythingQuery); err != nil {
		// The backup stays on disk and in the index, it is still valid
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	logRequest(r, "Database truncated, backup "+filename+" created")

	// Send backup file
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	http.ServeFile(w, r, zipPath) //nolint:gosec
}

//go:embed queries/replace_schema.sql.noformat
var replaceSchemaQueryTemplate string

//go:embed queries/restore_after_import_failure.sql
var restoreAfterImportFailureQuery string

//go:embed queries/delete_old_import_schema.sql
var deleteOldImportSchemaQuery string

//go:embed queries/copy_authorized_users_from_old_schema.sql
var copyAuthorizedUsersQuery string

//go:embed queries/truncate_everything.sql
var truncateEverythingQuery string

// loadAndMigrate renames the existing schema, loads the schema with the same version
// as the dump, migrates and finally deletes the old schema
func (s *Server) loadAndMigrate(r *http.Request, dumpData io.Reader, metadata *Metadata) error {
	password, conURL, err := getDBCredentials()
	if err != nil {
		return fmt.Errorf("get db credentials: %w", err)
	}

	// Load schema for the backup version from the embedded migrations.
	schemaSQL, err := migration.ReadSchema(metadata.SchemaVersion)
	if err != nil {
		return fmt.Errorf("read schema file: %w", err)
	}
	replaceSchemaQuery := strings.ReplaceAll(replaceSchemaQueryTemplate, "__NEW_SCHEMA_PLACEHOLDER__", string(schemaSQL))

	conn, err := s.DB.Acquire(r.Context())
	if err != nil {
		return fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()
	conn.Exec(r.Context(), "set lock_timeout = '5s'") // Don't get stuck forever trying to acquire an exclusive lock

	// Rename old public schema, then create new public schema with correct version
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	_, err = conn.Exec(ctx, replaceSchemaQuery)
	if err != nil {
		logRequest(r, "Couldn't replace schema")
		return fmt.Errorf("replace schema: %w", err)
	}

	// Run pg_restore
	restoreCmd := exec.CommandContext( //nolint:gosec
		ctx,
		"pg_restore",
		"--dbname",
		conURL,
		"--data-only",
		"--no-owner",
		"--no-privileges",
	)
	restoreCmd.Env = append(os.Environ(), "PGPASSWORD="+password)
	restoreCmd.Stdin = dumpData
	var stderr bytes.Buffer
	restoreCmd.Stderr = &stderr

	if err := restoreCmd.Run(); err != nil {
		// pg_restore failed, rollback to old schema and data
		if _, rollbackErr := conn.Exec(ctx, restoreAfterImportFailureQuery); rollbackErr != nil {
			return fmt.Errorf("rollback after import failure: %w", rollbackErr)
		}
		return fmt.Errorf("pg_restore: %w: %s", err, stderr.String())
	}

	// Migrate to newest schema version
	if err := migration.RunMigrationsConn(ctx, conn); err != nil {
		// Migration failed, rollback to old schema and data
		_, err = conn.Exec(ctx, restoreAfterImportFailureQuery)
		if err != nil {
			return fmt.Errorf("rollback after migration failure: %w", err)
		}
		return fmt.Errorf("run migrations: %w", err)
	}

	// Copy authorized users from the old schema, since they are excluded from dumps
	_, err = conn.Exec(ctx, copyAuthorizedUsersQuery)
	if err != nil {
		// Copy failed, rollback to old schema and data
		_, rollbackErr := conn.Exec(ctx, restoreAfterImportFailureQuery)
		if rollbackErr != nil {
			return fmt.Errorf("rollback after copying authorized users failed: %w", rollbackErr)
		}
		return fmt.Errorf("copy authorized users: %w", err)
	}

	// Delete old (renamed) schema
	_, err = conn.Exec(ctx, deleteOldImportSchemaQuery)
	if err != nil {
		return fmt.Errorf("delete old schema: %w", err)
	}

	logRequest(r, "Import succeeded")
	return nil
}

// addToZip adds and compresses a single file (path) to a zip.Writer
func addToZip(zw *zip.Writer, path string) error {
	// Use file info to create zip header
	src, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open source file: %w", err)
	}
	defer src.Close()

	info, err := src.Stat()
	if err != nil {
		return fmt.Errorf("stat source file: %w", err)
	}

	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return fmt.Errorf("create zip file info header: %w", err)
	}
	header.Name = filepath.Base(path)
	header.Method = zip.Deflate

	dst, err := zw.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("write zip file header: %w", err)
	}
	if _, err = io.Copy(dst, src); err != nil {
		return fmt.Errorf("copy / compress data into zip file: %w", err)
	}
	return nil
}

// extractFromZip extracts the metadata and dump file from a backup zip.
func extractFromZip(zipPath string) (*zip.ReadCloser, *Metadata, *zip.File, error) {
	rc, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("open zip: %w", err)
	}

	var metadata *Metadata
	var dumpFile *zip.File
	// Iterate through contained files to find metadata.json and the dump file
	for _, f := range rc.File {
		if f.Name == "metadata.json" {
			fr, err := f.Open()
			if err != nil {
				rc.Close()
				return nil, nil, nil, fmt.Errorf("open metadata.json in zip: %w", err)
			}
			err = json.NewDecoder(fr).Decode(&metadata)
			fr.Close()
			if err != nil {
				rc.Close()
				return nil, nil, nil, fmt.Errorf("decode metadata.json: %w", err)
			}
		}
		if filepath.Ext(f.Name) == ".stdp" {
			dumpFile = f
		}
	}

	if metadata == nil {
		rc.Close()
		return nil, nil, nil, fmt.Errorf("metadata.json not found in zip")
	}
	if dumpFile == nil {
		rc.Close()
		return nil, nil, nil, fmt.Errorf("dump file (.stdp) not found in zip")
	}

	return rc, metadata, dumpFile, nil
}

// --- Backups ---

// Handler: GET /backups/{id}; Download a single backup zip by ID
func (s *Server) getBackup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	index, err := getIndex()
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	entry, err := index.Get(id)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	zipPath := filepath.Join(persistentZipPath, entry.Filename+".zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, entry.Filename))
	http.ServeFile(w, r, zipPath)
}

// Handler: GET /backups; Get the index of all backups stored on the server
func (s *Server) getBackups(w http.ResponseWriter, r *http.Request) {
	index, err := getIndex()
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, index.Entries)
}

// Handler: DELETE /backups/{id}; Delete a backup from the server
func (s *Server) deleteBackup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	index, err := getIndex()
	if err != nil {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	entry, err := index.Remove(id)
	if err != nil {
		if err == errBackupNotFound {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	zipPath := filepath.Join(persistentZipPath, entry.Filename+".zip")
	if err := os.Remove(zipPath); err != nil && !os.IsNotExist(err) {
		logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
