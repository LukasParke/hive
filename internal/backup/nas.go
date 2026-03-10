package backup

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"go.uber.org/zap"
)

type NASBackupWriter struct {
	log *zap.SugaredLogger
}

func NewNASBackupWriter(log *zap.SugaredLogger) *NASBackupWriter {
	return &NASBackupWriter{log: log}
}

// CopyToNAS copies a backup file to a NAS mount path.
// The nasPath should be a locally-accessible path (e.g., an NFS volume mount).
func (w *NASBackupWriter) CopyToNAS(ctx context.Context, localFile, nasPath string) (string, error) {
	if err := os.MkdirAll(nasPath, 0755); err != nil {
		return "", fmt.Errorf("create NAS backup dir %s: %w", nasPath, err)
	}

	destFile := filepath.Join(nasPath, filepath.Base(localFile))

	src, err := os.Open(localFile)
	if err != nil {
		return "", fmt.Errorf("open source: %w", err)
	}
	defer func() { _ = src.Close() }()

	dst, err := os.Create(destFile)
	if err != nil {
		return "", fmt.Errorf("create dest: %w", err)
	}
	defer func() { _ = dst.Close() }()

	if _, err := io.Copy(dst, src); err != nil {
		return "", fmt.Errorf("copy to NAS: %w", err)
	}

	w.log.Infof("backup copied to NAS: %s", destFile)
	return destFile, nil
}

// EnforceRetention deletes backup files older than retentionDays in the given directory.
func (w *NASBackupWriter) EnforceRetention(dir string, retentionDays int) {
	if retentionDays <= 0 {
		return
	}

	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	entries, err := os.ReadDir(dir)
	if err != nil {
		w.log.Warnf("retention scan %s: %v", dir, err)
		return
	}

	type fileEntry struct {
		name    string
		modTime time.Time
	}
	var files []fileEntry
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, fileEntry{name: e.Name(), modTime: info.ModTime()})
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.Before(files[j].modTime)
	})

	for _, f := range files {
		if f.modTime.Before(cutoff) {
			path := filepath.Join(dir, f.name)
			if err := os.Remove(path); err != nil {
				w.log.Warnf("retention remove %s: %v", path, err)
			} else {
				w.log.Infof("retention: removed old backup %s", path)
			}
		}
	}
}
