package ctl

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestAcquireLockAndRelease(t *testing.T) {
	dir := t.TempDir()
	lock, err := AcquireLock(dir)
	if err != nil {
		t.Fatalf("AcquireLock failed: %v", err)
	}
	lockPath := filepath.Join(dir, ".datoriumctl.lock")
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("expected lock file to exist: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("Release failed: %v", err)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("expected lock file removed after release, err: %v", err)
	}
}

func TestAcquireLockFailsWhenHeld(t *testing.T) {
	dir := t.TempDir()
	lock, err := AcquireLock(dir)
	if err != nil {
		t.Fatalf("AcquireLock failed: %v", err)
	}
	defer lock.Release()

	_, err = AcquireLock(dir)
	if err == nil {
		t.Fatal("expected second AcquireLock to fail while lock is held")
	}
	var heldErr *LockHeldError
	if !errors.As(err, &heldErr) {
		t.Fatalf("expected LockHeldError, got %v (%T)", err, err)
	}
}

func TestAcquireLockCreatesConfigDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "config")
	lock, err := AcquireLock(dir)
	if err != nil {
		t.Fatalf("AcquireLock failed: %v", err)
	}
	defer lock.Release()
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("expected config dir created: %v", err)
	}
}
