// Package main configures the database pool
// and starts the api-server found in [stundenplan_api/apiserver].
package main

import (
	"context"
	"embed"
	"log"
	"net/http"
	"os"
	"stundenplan_api/apiserver"
	"stundenplan_api/migration"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	noAuth := os.Getenv("NO_AUTH") != ""
	dbURL := os.Getenv("DATABASE_URL")
	// not caught by ParseConfig since the empty string is also a valid url (with default values)
	if dbURL == "" {
		log.Fatal("Please set DATABASE_URL")
	}
	cfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		log.Fatal("Couldn't parse DB URL. Is DATABASE_URL set?", err)
	}

	// Override some of the config. Values are eyeballed, may need adjustment
	const maxConnLifetime = 20 * time.Minute
	const maxConnIdleTime = 5 * time.Minute
	cfg.MinConns = 2
	cfg.MaxConns = 20
	cfg.MaxConnLifetime = maxConnLifetime
	cfg.MaxConnIdleTime = maxConnIdleTime

	dbpool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		log.Fatal("Unable to connect to database. Is DATABASE_URL set? \n", err)
		return
	}
	defer dbpool.Close()

	// The database runs in its own stack and may start later, so wait for it
	const (
		pingTimeout   = 3 * time.Second
		retryInterval = 2 * time.Second
		maxAttempts   = 30
	)
	for attempt := 1; ; attempt++ {
		pingCtx, cancelPing := context.WithTimeout(context.Background(), pingTimeout)
		err = dbpool.Ping(pingCtx)
		cancelPing()
		if err == nil {
			break
		}
		if attempt == maxAttempts {
			log.Fatalf("Unable to connect to database: %s\n", err)
		}
		log.Printf("Database not reachable yet (attempt %d/%d): %s\n", attempt, maxAttempts, err)
		time.Sleep(retryInterval)
	}

	// timeout might need to be increased if migrating many versions at once
	migrateCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	migrate(migrateCtx, dbpool)

	const readHeaderTimeout = 8 * time.Second
	apiServer := apiserver.New(dbpool, noAuth)
	server := &http.Server{
		Addr:              ":8004",
		Handler:           apiServer.Handler(),
		ReadHeaderTimeout: readHeaderTimeout, // Prevent Slowloris attack
	}

	log.Printf("Listening on http://localhost:8004\n")
	log.Panic(server.ListenAndServe())
}

//go:embed db/migrations/*
var migrationsFS embed.FS

func migrate(ctx context.Context, pool *pgxpool.Pool) {
	migration.SetMigrationFs(migrationsFS)
	if err := migration.RunMigrations(ctx, pool); err != nil {
		log.Fatalf("Unable to run migrations: %s\n", err)
	}
}
