package db

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

func MigrateRiver(ctx context.Context, pool *pgxpool.Pool) error {
	driver := riverpgxv5.New(pool)
	migrator, err := rivermigrate.New(driver, nil)
	if err != nil {
		return fmt.Errorf("rivermigrate init: %w", err)
	}
	_, err = migrator.Migrate(ctx, rivermigrate.DirectionUp, &rivermigrate.MigrateOpts{})
	if err != nil {
		return fmt.Errorf("river migrate: %w", err)
	}
	return nil
}

func ApplyMigrations(ctx context.Context, pool *pgxpool.Pool, root fs.FS) error {
	if _, err := pool.Exec(ctx, `
		create table if not exists schema_migrations (
			version text primary key,
			applied_at timestamptz not null default now()
		)`); err != nil {
		return err
	}

	entries, err := fs.ReadDir(root, ".")
	if err != nil {
		return err
	}

	var upFiles []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".up.sql") {
			upFiles = append(upFiles, name)
		}
	}
	sort.Strings(upFiles)

	for _, name := range upFiles {
		var exists bool
		if err := pool.QueryRow(ctx, "select exists(select 1 from schema_migrations where version=$1)", name).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}

		sqlBytes, err := fs.ReadFile(root, name)
		if err != nil {
			return err
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, string(sqlBytes)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("migration %s failed: %w", name, err)
		}
		if _, err := tx.Exec(ctx, "insert into schema_migrations(version) values ($1)", name); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}
