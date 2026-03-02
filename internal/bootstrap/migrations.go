package bootstrap

import (
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

func (b *Bootstrapper) runMigrations() error {
	b.log.Info("running database migrations")

	migrationsPath := "file:///app/migrations"
	if b.cfg.DevMode {
		migrationsPath = "file://internal/store/migrations"
	}

	m, err := migrate.New(migrationsPath, b.cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("migrations init: %w", err)
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("migrations up: %w", err)
	}

	b.log.Info("database migrations complete")
	return nil
}
