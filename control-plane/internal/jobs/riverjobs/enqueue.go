package riverjobs

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
)

// ErrBuildAlreadyQueued is returned when the database rejects the build
// because the partial unique index idx_build_jobs_active_per_application
// already sees a queued/building job for the application.
var ErrBuildAlreadyQueued = errors.New("a build is already queued or running for this application")

// IsUniqueViolation reports whether err is a Postgres unique constraint
// violation (SQLSTATE 23505).
func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// EnqueueBuild inserts the build_jobs row for the application and enqueues
// the matching River build job. imageTag pre-selects an image for rollback
// builds; empty otherwise. Returns ErrBuildAlreadyQueued when the unique
// index blocks a duplicate active build.
func EnqueueBuild(ctx context.Context, client *river.Client[pgx.Tx], pool *pgxpool.Pool, appID, trigger, imageTag string) (string, error) {
	var buildID string
	if err := pool.QueryRow(ctx, `
		insert into build_jobs(application_id, trigger, status, image_tag)
		values ($1::uuid, $2, 'queued', nullif($3, ''))
		returning id::text
	`, appID, trigger, imageTag).Scan(&buildID); err != nil {
		if IsUniqueViolation(err) {
			return "", ErrBuildAlreadyQueued
		}
		return "", fmt.Errorf("insert build job: %w", err)
	}

	if client != nil {
		if _, err := client.Insert(ctx, BuildJobArgs{BuildID: buildID}, nil); err != nil {
			// Nothing will execute the row anymore; fail it visibly.
			_, _ = pool.Exec(ctx,
				`update build_jobs set status='failed', error_message=$2, completed_at=now() where id=$1::uuid`,
				buildID, "enqueue failed: "+err.Error())
			return "", fmt.Errorf("enqueue build job: %w", err)
		}
	}
	return buildID, nil
}
