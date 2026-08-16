package main

import (
	"context"
	"fmt"
	"os"

	"github.com/kakj-go/Argus/internal/storage/postgres"
)

func main() {
	if len(os.Args) != 2 || (os.Args[1] != "up" && os.Args[1] != "down") {
		fmt.Fprintln(os.Stderr, "usage: argus-migrate up|down")
		os.Exit(2)
	}
	databaseURL := os.Getenv("ARGUS_DATABASE_URL")
	if databaseURL == "" {
		fmt.Fprintln(os.Stderr, "ARGUS_DATABASE_URL is required")
		os.Exit(1)
	}
	directory := os.Getenv("ARGUS_MIGRATIONS_DIR")
	if directory == "" {
		directory = "migrations/postgresql"
	}
	if err := postgres.RunMigrations(context.Background(), databaseURL, directory, postgres.MigrationDirection(os.Args[1])); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
