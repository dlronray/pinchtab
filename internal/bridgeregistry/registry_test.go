package bridgeregistry

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRegisterRejectsLiveDuplicateAndRemove(t *testing.T) {
	t.Setenv(registryDirEnv, t.TempDir())
	first, err := Register("ws://127.0.0.1:9222/devtools/browser/test", "19871")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	_, err = Register(first.Target, "19872")
	var duplicate *LiveTargetError
	if !errors.As(err, &duplicate) {
		t.Fatalf("duplicate Register() error = %v, want LiveTargetError", err)
	}
	if duplicate.Entry.Port != first.Port {
		t.Fatalf("duplicate port = %q, want %q", duplicate.Entry.Port, first.Port)
	}

	entries := List()
	if len(entries) != 1 || entries[0].PID != os.Getpid() {
		t.Fatalf("List() = %#v, want current bridge", entries)
	}
	if err := Remove(first); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if entries := List(); len(entries) != 0 {
		t.Fatalf("List() after Remove = %#v, want empty", entries)
	}
}

func TestListRemovesDeadPID(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(registryDirEnv, dir)
	entry := Entry{
		PID:       2147483647,
		Port:      "19871",
		Target:    "ws://127.0.0.1:9222/devtools/browser/stale",
		Cwd:       "/stale",
		StartTime: time.Now().Add(-time.Hour),
	}
	path := entryPath(dir, entry.Target)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(f).Encode(entry); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	if entries := List(); len(entries) != 0 {
		t.Fatalf("List() = %#v, want stale entry removed", entries)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale registry file still exists: %v", err)
	}
}

func TestRegisterReplacesMalformedEntry(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(registryDirEnv, dir)
	target := "ws://127.0.0.1:9222/devtools/browser/recover"
	if err := os.WriteFile(entryPath(dir, target), []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}

	entry, err := Register(target, "19873")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	t.Cleanup(func() { _ = Remove(entry) })
	if _, err := os.Stat(filepath.Join(dir, filepath.Base(entryPath(dir, target)))); err != nil {
		t.Fatalf("replacement registry file missing: %v", err)
	}
}
