// migration contains the database schema version migration logic.
// A migration is run when needed on program startup or when importing an old database dump.
package migration

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"slices"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed max_schema_version.sql
var maxSchemaVersion string

var migrationsFS embed.FS

type migrationEntry struct {
	value int
	entry fs.DirEntry
}

func SetMigrationFs(fs embed.FS) {
	// I know this is ugly, but I need access to RunMigrations and the schema files in import_export_dump.go,
	// so this needs to be in its own package.
	// But go:embed doesn't allow to access parent directories, so I have to pass the embeds from main.
	// Alternatively, we could move db/migrations into the migration package.
	migrationsFS = fs
}

// ReadSchema returns the embedded schema.sql for the given schema version
func ReadSchema(version int) ([]byte, error) {
	if migrationsFS == (embed.FS{}) {
		return nil, fmt.Errorf("migration filesystem not set")
	}
	return migrationsFS.ReadFile(fmt.Sprintf("db/migrations/%03d/schema.sql", version))
}

// RunMigrationsConn migrates the DB schema to the latest version.
// No action if already up to date.
func RunMigrationsConn(ctx context.Context, conn *pgxpool.Conn) error {
	if migrationsFS == (embed.FS{}) {
		return fmt.Errorf("migration filesystem not set")
	}
	var currentVersion int // Current schema version, as read from the db
	if err := conn.QueryRow(ctx, maxSchemaVersion).Scan(&currentVersion); err != nil {
		return fmt.Errorf("Error fetching maximum schema version: %w", err)
	}

	var migrations []migrationEntry

	entries, err := migrationsFS.ReadDir("db/migrations")

	if err != nil {
		return fmt.Errorf("Error reading migrations fs: %w", err)
	}

	for _, entry := range entries {
		value, err := strconv.Atoi(entry.Name())
		if err != nil || !entry.IsDir() {
			return fmt.Errorf("Found non migration %s in migrations fs; name must be a valid integer and a directory", entry.Name())
		}

		migrations = append(migrations, migrationEntry{value: value, entry: entry})
	}

	if len(migrations) == 0 {
		return fmt.Errorf("No migrations found")
	}

	// probably already sorted but here just in case
	slices.SortFunc(migrations, func(a migrationEntry, b migrationEntry) int { return a.value - b.value })

	idx, found := slices.BinarySearchFunc(migrations, currentVersion, func(a migrationEntry, b int) int { return a.value - b })

	if !found && currentVersion != 0 {
		return fmt.Errorf("Did not find current schema version %d in migrations", currentVersion)
	}

	if idx == len(migrations)-1 && currentVersion != 0 {
		return nil
	}

	log.Printf("Migration from schema version %d to schema version %d\n", currentVersion, migrations[len(migrations)-1].value)

	migrationTx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("Error starting migration transaction: %w", err)
	}
	defer migrationTx.Rollback(ctx)

	var startIndex int

	if currentVersion == 0 {
		startIndex = 0
	} else {
		startIndex = idx + 1
	}

	for _, migration := range migrations[startIndex:] {
		data, err := migrationsFS.ReadFile(fmt.Sprintf("db/migrations/%s/up.sql", migration.entry.Name()))
		if err != nil {
			return fmt.Errorf("Error reading migration %s's up.sql: %w", migration.entry.Name(), err)
		}

		if _, err = migrationTx.Exec(ctx, string(data)); err != nil {
			return fmt.Errorf("Error applying migration %s: %w", migration.entry.Name(), err)
		}
	}

	if err = migrationTx.Commit(ctx); err != nil {
		return fmt.Errorf("Error commiting migration transaction: %w", err)
	}

	return nil
}

// RunMigrations wraps RunMigrationsConn and uses a pgxpool.Pool to acquire the connection
func RunMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("Error acquiring connection for migrations: %w", err)
	}
	defer conn.Release()

	return RunMigrationsConn(ctx, conn)
}
