package postgres

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

type MigrationDirection string

const (
	MigrationUp   MigrationDirection = "up"
	MigrationDown MigrationDirection = "down"
)

func RunMigrations(ctx context.Context, databaseURL, directory string, direction MigrationDirection) error {
	if direction != MigrationUp && direction != MigrationDown {
		return fmt.Errorf("unsupported migration direction %q", direction)
	}
	database, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return err
	}
	defer database.Close()
	connection, err := database.Conn(ctx)
	if err != nil {
		return err
	}
	defer connection.Close()
	if _, err := connection.ExecContext(ctx, "SELECT pg_advisory_lock(hashtext('argus-postgresql-migration'))"); err != nil {
		return err
	}
	defer func() {
		_, _ = connection.ExecContext(context.Background(), "SELECT pg_advisory_unlock(hashtext('argus-postgresql-migration'))")
	}()
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	switch direction {
	case MigrationUp:
		return goose.Up(database, directory)
	case MigrationDown:
		return goose.Down(database, directory)
	default:
		return nil
	}
}
