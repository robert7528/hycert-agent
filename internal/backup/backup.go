package backup

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// File backs up a file to backupDir with a timestamp suffix.
// Returns the backup path, or empty string if source doesn't exist.
func File(srcPath, backupDir string) (string, error) {
	if _, err := os.Stat(srcPath); os.IsNotExist(err) {
		return "", nil // nothing to back up
	}

	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return "", fmt.Errorf("create backup dir: %w", err)
	}

	ts := time.Now().Format("20060102-150405")
	base := filepath.Base(srcPath)
	backupPath := filepath.Join(backupDir, fmt.Sprintf("%s.%s", base, ts))

	src, err := os.Open(srcPath)
	if err != nil {
		return "", fmt.Errorf("open source: %w", err)
	}
	defer src.Close()

	dst, err := os.OpenFile(backupPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return "", fmt.Errorf("create backup: %w", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return "", fmt.Errorf("copy backup: %w", err)
	}

	return backupPath, nil
}
