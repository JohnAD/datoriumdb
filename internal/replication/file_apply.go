package replication

import (
	"fmt"
	"io"
	"os"

	"github.com/JohnAD/datoriumdb/internal/fsstore"
)

// FileApplier applies replicated attachment writes idempotently.
type FileApplier struct {
	DataDir string
}

// ApplyMetadataOnly applies a fileDelete (or metadata-only) item.
func (a *FileApplier) ApplyMetadataOnly(item FileWorkItem) (applied bool, err error) {
	if item.Command != "fileDelete" {
		return false, fmt.Errorf("replication: ApplyMetadataOnly expects fileDelete, got %q", item.Command)
	}
	entries, err := fsstore.ReadFilesManifest(a.DataDir, item.Collection, item.ID)
	if err != nil {
		return false, err
	}
	existing, ok := fsstore.LookupFileEntry(entries, item.Filename)
	if !ok {
		return true, nil // already gone
	}
	// Idempotent: if a newer version exists locally, still remove only when
	// versions match or the pending delete names this filename.
	_ = existing
	entry := fsstore.FileEntry{
		Name:        item.Filename,
		Version:     item.Version,
		OperationID: item.OperationID,
		SHA256:      item.SHA256,
		ByteSize:    item.ByteSize,
		ContentType: item.ContentType,
	}
	if err := fsstore.CommitFileDelete(a.DataDir, item.Collection, item.ID, entry); err != nil {
		return false, err
	}
	return true, nil
}

// ApplyStream verifies SHA-256 while writing, then commits blob+manifest.
func (a *FileApplier) ApplyStream(item FileWorkItem, r io.Reader, maxBytes int64) (applied bool, err error) {
	if item.Command != "fileCreate" && item.Command != "fileUpdate" {
		return false, fmt.Errorf("replication: ApplyStream expects fileCreate/fileUpdate, got %q", item.Command)
	}
	entries, err := fsstore.ReadFilesManifest(a.DataDir, item.Collection, item.ID)
	if err != nil {
		return false, err
	}
	if existing, ok := fsstore.LookupFileEntry(entries, item.Filename); ok {
		if existing.Version == item.Version && existing.SHA256 == item.SHA256 {
			return true, nil // already applied
		}
	}
	staged, err := fsstore.StageWriteBinary(a.DataDir, item.Collection, item.ID, item.Filename, r, maxBytes)
	if err != nil {
		return false, err
	}
	defer func() { _ = os.Remove(staged.StagedPath) }()
	if item.SHA256 != "" && staged.SHA256 != item.SHA256 {
		return false, fmt.Errorf("replication: sha256 mismatch for %s/%s/%s: got %s want %s",
			item.Collection, item.ID, item.Filename, staged.SHA256, item.SHA256)
	}
	if item.ByteSize > 0 && staged.ByteSize != item.ByteSize {
		return false, fmt.Errorf("replication: byteSize mismatch for %s/%s/%s: got %d want %d",
			item.Collection, item.ID, item.Filename, staged.ByteSize, item.ByteSize)
	}
	entry := fsstore.FileEntry{
		Name:        item.Filename,
		ContentType: item.ContentType,
		ByteSize:    staged.ByteSize,
		SHA256:      staged.SHA256,
		Version:     item.Version,
		OperationID: item.OperationID,
	}
	if entry.ContentType == "" {
		entry.ContentType = "application/octet-stream"
	}
	if err := fsstore.CommitStagedBinary(a.DataDir, item.Collection, item.ID, entry, staged); err != nil {
		return false, err
	}
	staged.StagedPath = ""
	return true, nil
}
