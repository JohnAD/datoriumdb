package ctl

import (
	"fmt"
	"os"
	"path/filepath"
)

// Lock is an exclusive, non-blocking lock file at
// {config-dir}/.datoriumctl.lock.
type Lock struct {
	path string
}

// LockHeldError reports that the config directory lock is already held.
type LockHeldError struct {
	Path string
}

func (e *LockHeldError) Error() string {
	return fmt.Sprintf("config lock already held: %s", e.Path)
}

// AcquireLock creates the lock file exclusively. If it already exists, the
// caller should fail immediately rather than waiting.
func AcquireLock(configDir string) (*Lock, error) {
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(configDir, ".datoriumctl.lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return nil, &LockHeldError{Path: path}
		}
		return nil, err
	}
	fmt.Fprintf(f, "%d\n", os.Getpid())
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	return &Lock{path: path}, nil
}

// Release removes the lock file.
func (l *Lock) Release() error {
	if l == nil {
		return nil
	}
	return os.Remove(l.path)
}
