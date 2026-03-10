package store

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq"
)

type Store struct {
	db *sql.DB
}

// New opens a connection to Postgres with retry/backoff for failover resilience.
// Connections are recycled aggressively (1 min lifetime) so stale connections
// to a demoted standby are replaced quickly after a repmgr failover.
func New(databaseURL string) (*Store, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(1 * time.Minute)
	db.SetConnMaxIdleTime(30 * time.Second)

	var lastErr error
	for attempt := 1; attempt <= 5; attempt++ {
		if err := db.Ping(); err != nil {
			lastErr = err
			backoff := time.Duration(attempt) * 2 * time.Second
			time.Sleep(backoff)
			continue
		}
		return &Store{db: db}, nil
	}
	return nil, fmt.Errorf("ping db after 5 attempts: %w", lastErr)
}

func NewFromDB(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) DB() *sql.DB {
	return s.db
}

func (s *Store) Close() error {
	return s.db.Close()
}
