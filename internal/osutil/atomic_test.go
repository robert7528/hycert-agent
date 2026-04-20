package osutil

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWriteFileAtomic_CreatesFileWithContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cert.pem")
	content := []byte("hello cert")

	if err := WriteFileAtomic(path, content, 0644); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("content mismatch: got %q want %q", got, content)
	}
}

func TestWriteFileAtomic_RespectsModeAtCreation(t *testing.T) {
	// On Windows, Go's os.FileMode only reflects read-only bit for files, so
	// 0600 vs 0644 permission checks are Unix-only.
	if runtime.GOOS == "windows" {
		t.Skip("file mode semantics differ on Windows")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "key.pem")

	if err := WriteFileAtomic(path, []byte("secret"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("permission = %o, want 0600", perm)
	}
}

func TestWriteFileAtomic_OverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cert.pem")

	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := WriteFileAtomic(path, []byte("new"), 0644); err != nil {
		t.Fatalf("overwrite: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "new" {
		t.Errorf("got %q want %q", got, "new")
	}
}

func TestWriteFileAtomic_CleansTempOnWriteFailure(t *testing.T) {
	// Target a non-writable directory to force failure after tmp creation.
	// Skip on Windows where permission semantics are different.
	if runtime.GOOS == "windows" {
		t.Skip("permission-based failure test is Unix-only")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}

	dir := t.TempDir()
	// Make dir read-only so tmp file creation fails.
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer os.Chmod(dir, 0700)

	path := filepath.Join(dir, "cert.pem")
	err := WriteFileAtomic(path, []byte("x"), 0600)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "create temp") {
		t.Errorf("expected 'create temp' error, got: %v", err)
	}

	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".hycert-tmp-") {
			t.Errorf("orphan temp file left behind: %s", e.Name())
		}
	}
}

func TestWriteFileAtomic_NoOrphanTempOnSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cert.pem")

	if err := WriteFileAtomic(path, []byte("ok"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".hycert-tmp-") {
			t.Errorf("unexpected temp file: %s", e.Name())
		}
	}
}
