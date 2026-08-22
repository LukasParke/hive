package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/luke/hive/control-plane/internal/config"
	"github.com/luke/hive/control-plane/internal/db"
	"github.com/luke/hive/control-plane/internal/secrets"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx := context.Background()
	cfg := config.Load()

	pool, err := db.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	key, err := os.ReadFile(cfg.MasterKeyFile)
	if err != nil {
		return err
	}
	store, err := secrets.NewStore(pool, []byte(strings.TrimSpace(string(key))))
	if err != nil {
		return err
	}

	roots := map[string]string{
		"/data/.ssh":   "ssh_key",
		"/data/certs":  "tls_key",
		"/data/tls":    "tls_cert",
		"/data/tokens": "signing_key",
	}

	for root, typ := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			path := filepath.Join(root, entry.Name())
			raw, err := os.ReadFile(path) //nolint:gosec // by-design sweep of the operator-configured secret directories
			if err != nil {
				log.Printf("skip %s: %v", path, err)
				continue
			}
			if err := store.Put(ctx, entry.Name(), typ, raw); err != nil {
				log.Printf("store %s failed: %v", path, err)
				continue
			}
			if err := os.Remove(path); err != nil {
				log.Printf("stored but could not remove %s: %v", path, err)
			}
		}
	}
	return nil
}
