// Package osutil provides low-level OS helpers shared across the agent.
package osutil

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// WriteFileAtomic writes data to path using a write-tmp-then-rename pattern
// with explicit mode at creation time (no permission race window).
//
// Steps:
//  1. Create temp file in the target's parent directory with O_CREAT|O_EXCL
//     and the requested mode — no window where the private key is 0644.
//  2. Write content, fsync the file, close.
//  3. Rename temp → final (atomic on POSIX and Windows NTFS).
//  4. fsync the parent directory (POSIX only — Windows NTFS has its own
//     write ordering guarantees; directory-handle FlushFileBuffers is
//     unsupported on Windows and returns an error we deliberately ignore).
//
// mode is not defaulted intentionally — the caller must pick 0600 (private
// keys, keystores) vs 0644 (public certs) explicitly.
//
// Orphan-temp safety: on any failure between tmp creation and rename, the
// temp file is removed. On success, the rename makes tmpName nonexistent,
// so the deferred cleanup is a no-op (ENOENT is ignored).
func WriteFileAtomic(path string, data []byte, mode os.FileMode) (err error) {
	dir := filepath.Dir(path)

	tmp, tmpName, err := createTempFile(dir, mode)
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	// Named-return cleanup: remove on any error path before rename succeeds.
	// After successful rename, tmpName doesn't exist and Remove is a no-op.
	defer func() {
		if err != nil {
			os.Remove(tmpName)
		}
	}()

	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp: %w", err)
	}
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("fsync temp: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}

	if err = os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename %s → %s: %w", tmpName, path, err)
	}

	// fsync parent directory so the rename is durable. Windows does not
	// support directory-handle sync; NTFS orders metadata writes itself.
	if runtime.GOOS != "windows" {
		if d, openErr := os.Open(dir); openErr == nil {
			_ = d.Sync()
			d.Close()
		}
	}

	return nil
}

// createTempFile makes a unique tmp file in dir with the exact mode at
// creation time. Retries once on EEXIST in the extremely unlikely case of a
// name collision (pid + crypto random suffix).
func createTempFile(dir string, mode os.FileMode) (*os.File, string, error) {
	for attempt := 0; attempt < 2; attempt++ {
		name := filepath.Join(dir, tmpName())
		f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
		if err == nil {
			return f, name, nil
		}
		if !os.IsExist(err) {
			return nil, "", err
		}
		// collision, retry once with a fresh suffix
	}
	return nil, "", fmt.Errorf("could not create unique temp file in %s", dir)
}

func tmpName() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf(".hycert-tmp-%d-%s", os.Getpid(), hex.EncodeToString(b[:]))
}
