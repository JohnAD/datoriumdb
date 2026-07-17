package fsstore

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"time"
)

// WriteFileAtomic writes data to path using a same-directory temp file and rename.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("atomic rename to %s: %w", path, err)
	}
	cleanup = false
	if dirFile, err := os.Open(dir); err == nil {
		_ = dirFile.Sync()
		_ = dirFile.Close()
	}
	return nil
}

// VerifyDocumentVersion reads path and checks that doc["#"] equals expected.
func VerifyDocumentVersion(path, expected string) error {
	doc, err := ReadDocumentJSON(path)
	if err != nil {
		return err
	}
	actual, _ := doc["#"].(string)
	if actual != expected {
		return fmt.Errorf("version mismatch after write: expected %s actual %s", expected, actual)
	}
	return nil
}

// WriteDocumentJSONVerified atomically writes doc and retries brief verification of "#".
func WriteDocumentJSONVerified(path string, doc map[string]any) error {
	version, _ := doc["#"].(string)
	if err := WriteDocumentJSON(path, doc); err != nil {
		return err
	}
	if version == "" {
		return nil
	}
	var last error
	for attempt := 0; attempt < 5; attempt++ {
		if err := VerifyDocumentVersion(path, version); err == nil {
			return nil
		} else {
			last = err
		}
		jitter := time.Duration(10+rand.Intn(40)) * time.Millisecond
		time.Sleep(jitter)
	}
	return fmt.Errorf("post-rename verification failed: %w", last)
}
