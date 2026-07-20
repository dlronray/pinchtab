package bridgeregistry

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
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

func TestStopTerminatesRegisteredBridgeAndRemovesEntry(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(registryDirEnv, dir)
	ready := filepath.Join(dir, "ready")
	cmd := exec.Command(os.Args[0], "-test.run=TestBridgeHelperProcess")
	cmd.Env = append(os.Environ(), registryDirEnv+"="+dir, "PINCHTAB_BRIDGE_TEST_HELPER=1", "PINCHTAB_BRIDGE_TEST_READY="+ready)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		<-waited
	})

	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("helper did not register bridge")
		}
		time.Sleep(10 * time.Millisecond)
	}
	entries := List()
	if len(entries) != 1 {
		t.Fatalf("List() = %#v, want helper bridge", entries)
	}
	if err := Stop(entries[0].ID()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if entries := List(); len(entries) != 0 {
		t.Fatalf("List() after Stop = %#v, want empty", entries)
	}
}

func TestBridgeHelperProcess(t *testing.T) {
	if os.Getenv("PINCHTAB_BRIDGE_TEST_HELPER") != "1" {
		return
	}
	if _, err := Register("ws://127.0.0.1:9222/devtools/browser/stop-test", ""); err != nil {
		os.Exit(2)
	}
	if err := os.WriteFile(os.Getenv("PINCHTAB_BRIDGE_TEST_READY"), []byte("ready"), 0o600); err != nil {
		os.Exit(3)
	}
	select {}
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
