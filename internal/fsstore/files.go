package fsstore

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// DefaultMaxFileBytes is the default streamed upload limit (1 GiB).
const DefaultMaxFileBytes int64 = 1 << 30

// MaxFileNameBytes is NAME_MAX for a single path component on common filesystems.
const MaxFileNameBytes = 255

// FileEntry is one current attachment described in {docId}__files.jsonl.
// JSON field order is fixed: name, contentType, byteSize, sha256, version, operationId.
type FileEntry struct {
	Name        string `json:"name"`
	ContentType string `json:"contentType"`
	ByteSize    int64  `json:"byteSize"`
	SHA256      string `json:"sha256"`
	Version     string `json:"version"`
	OperationID string `json:"operationId"`
}

// LFSDir returns {dataDir}/{collection}/lfs/{docId}.
func LFSDir(dataDir, collection, docID string) string {
	return filepath.Join(CollectionDir(dataDir, collection), "lfs", docID)
}

// BinaryPath returns the on-disk path for one attachment filename.
func BinaryPath(dataDir, collection, docID, filename string) string {
	return filepath.Join(LFSDir(dataDir, collection, docID), filename)
}

// FilesManifestPath returns {dataDir}/{collection}/{docId}__files.jsonl.
func FilesManifestPath(dataDir, collection, docID string) string {
	return filepath.Join(CollectionDir(dataDir, collection), docID+"__files.jsonl")
}

// FileOpsDir returns the durable local file-operation journal directory
// for one document: {collection}/.fileOps/{docId}.
func FileOpsDir(dataDir, collection, docID string) string {
	return filepath.Join(CollectionDir(dataDir, collection), ".fileOps", docID)
}

// PendingFileWritesDir returns {collection}/.pendingFileWrites.
func PendingFileWritesDir(dataDir, collection string) string {
	return filepath.Join(CollectionDir(dataDir, collection), ".pendingFileWrites")
}

// SafeFileName reports whether name is a single safe basename for attachment
// storage. Names are preserved exactly; only path-unsafe and control forms
// are rejected.
func SafeFileName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if strings.HasPrefix(name, ".") {
		return false
	}
	if !utf8.ValidString(name) {
		return false
	}
	if len(name) > MaxFileNameBytes {
		return false
	}
	if strings.Contains(name, "/") || strings.Contains(name, "\\") || strings.Contains(name, string(os.PathSeparator)) {
		return false
	}
	if filepath.Base(name) != name {
		return false
	}
	for _, r := range name {
		if r == 0 || r < 0x20 || r == 0x7f {
			return false
		}
	}
	cleaned := filepath.Clean(name)
	if cleaned != name || strings.HasPrefix(cleaned, "..") {
		return false
	}
	return true
}

// DocumentExistsLive reports whether the live parent document JSON exists.
func DocumentExistsLive(dataDir, collection, docID string) bool {
	_, err := os.Stat(DocumentPath(dataDir, collection, docID))
	return err == nil
}

// ReadFilesManifest loads the current attachment manifest. Missing file is an empty list.
func ReadFilesManifest(dataDir, collection, docID string) ([]FileEntry, error) {
	path := FilesManifestPath(dataDir, collection, docID)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []FileEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e FileEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return nil, fmt.Errorf("invalid files manifest line %d: %w", lineNo, err)
		}
		out = append(out, e)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// LookupFileEntry returns the manifest entry for filename, if any.
func LookupFileEntry(entries []FileEntry, filename string) (FileEntry, bool) {
	for _, e := range entries {
		if e.Name == filename {
			return e, true
		}
	}
	return FileEntry{}, false
}

// WriteFilesManifest atomically rewrites the manifest with fixed field order
// and lines sorted by name.
func WriteFilesManifest(dataDir, collection, docID string, entries []FileEntry) error {
	sorted := append([]FileEntry(nil), entries...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	var b strings.Builder
	for _, e := range sorted {
		line, err := marshalFileEntryLine(e)
		if err != nil {
			return err
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	path := FilesManifestPath(dataDir, collection, docID)
	if len(sorted) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return WriteFileAtomic(path, []byte(b.String()), 0o644)
}

func marshalFileEntryLine(e FileEntry) (string, error) {
	// Fixed field order; do not use encoding/json map marshaling.
	var b strings.Builder
	b.WriteByte('{')
	writeJSONString(&b, "name")
	b.WriteByte(':')
	if err := writeQuoted(&b, e.Name); err != nil {
		return "", err
	}
	b.WriteByte(',')
	writeJSONString(&b, "contentType")
	b.WriteByte(':')
	if err := writeQuoted(&b, e.ContentType); err != nil {
		return "", err
	}
	b.WriteByte(',')
	writeJSONString(&b, "byteSize")
	b.WriteByte(':')
	b.WriteString(fmt.Sprintf("%d", e.ByteSize))
	b.WriteByte(',')
	writeJSONString(&b, "sha256")
	b.WriteByte(':')
	if err := writeQuoted(&b, e.SHA256); err != nil {
		return "", err
	}
	b.WriteByte(',')
	writeJSONString(&b, "version")
	b.WriteByte(':')
	if err := writeQuoted(&b, e.Version); err != nil {
		return "", err
	}
	b.WriteByte(',')
	writeJSONString(&b, "operationId")
	b.WriteByte(':')
	if err := writeQuoted(&b, e.OperationID); err != nil {
		return "", err
	}
	b.WriteByte('}')
	return b.String(), nil
}

func writeJSONString(b *strings.Builder, s string) {
	_ = writeQuoted(b, s)
}

func writeQuoted(b *strings.Builder, s string) error {
	enc, err := json.Marshal(s)
	if err != nil {
		return err
	}
	b.Write(enc)
	return nil
}

// StreamWriteBinaryResult is the outcome of streaming a binary into LFS storage.
type StreamWriteBinaryResult struct {
	ByteSize int64
	SHA256   string
	Path     string
}

// StreamWriteBinary writes r to a temp file under the document LFS directory,
// enforces maxBytes, computes SHA-256, fsyncs, and atomically renames into place.
func StreamWriteBinary(dataDir, collection, docID, filename string, r io.Reader, maxBytes int64) (StreamWriteBinaryResult, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxFileBytes
	}
	if !SafeFileName(filename) {
		return StreamWriteBinaryResult{}, fmt.Errorf("invalid file name")
	}
	dir := LFSDir(dataDir, collection, docID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return StreamWriteBinaryResult{}, err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return StreamWriteBinaryResult{}, err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		_ = tmp.Close()
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()

	h := sha256.New()
	limited := &countingReader{r: io.TeeReader(r, h), max: maxBytes}
	n, err := io.Copy(tmp, limited)
	if err != nil {
		return StreamWriteBinaryResult{}, err
	}
	if err := tmp.Sync(); err != nil {
		return StreamWriteBinaryResult{}, err
	}
	if err := tmp.Close(); err != nil {
		return StreamWriteBinaryResult{}, err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return StreamWriteBinaryResult{}, err
	}
	final := BinaryPath(dataDir, collection, docID, filename)
	if err := os.Rename(tmpName, final); err != nil {
		return StreamWriteBinaryResult{}, fmt.Errorf("atomic rename to %s: %w", final, err)
	}
	cleanup = false
	if dirFile, err := os.Open(dir); err == nil {
		_ = dirFile.Sync()
		_ = dirFile.Close()
	}
	return StreamWriteBinaryResult{
		ByteSize: n,
		SHA256:   hex.EncodeToString(h.Sum(nil)),
		Path:     final,
	}, nil
}

type countingReader struct {
	r   io.Reader
	n   int64
	max int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	if c.n >= c.max {
		return 0, fmt.Errorf("fileTooLarge")
	}
	if int64(len(p)) > c.max-c.n {
		p = p[:c.max-c.n]
	}
	n, err := c.r.Read(p)
	c.n += int64(n)
	if c.n > c.max {
		return n, fmt.Errorf("fileTooLarge")
	}
	return n, err
}

// OpenBinary opens an attachment for streaming download.
func OpenBinary(dataDir, collection, docID, filename string) (*os.File, error) {
	if !SafeFileName(filename) {
		return nil, fmt.Errorf("invalid file name")
	}
	return os.Open(BinaryPath(dataDir, collection, docID, filename))
}

// RemoveBinary deletes one attachment file. Missing is success.
func RemoveBinary(dataDir, collection, docID, filename string) error {
	path := BinaryPath(dataDir, collection, docID, filename)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// CascadeDeleteDocumentFiles removes the document's LFS tree and files
// manifest. Idempotent: already-absent paths are success.
func CascadeDeleteDocumentFiles(dataDir, collection, docID string) error {
	manifest := FilesManifestPath(dataDir, collection, docID)
	if err := os.Remove(manifest); err != nil && !os.IsNotExist(err) {
		return err
	}
	lfs := LFSDir(dataDir, collection, docID)
	if err := os.RemoveAll(lfs); err != nil {
		return err
	}
	ops := FileOpsDir(dataDir, collection, docID)
	if err := os.RemoveAll(ops); err != nil {
		return err
	}
	return nil
}

// UpsertManifestEntry replaces or inserts one entry by name and rewrites the manifest.
func UpsertManifestEntry(dataDir, collection, docID string, entry FileEntry) error {
	entries, err := ReadFilesManifest(dataDir, collection, docID)
	if err != nil {
		return err
	}
	found := false
	for i, e := range entries {
		if e.Name == entry.Name {
			entries[i] = entry
			found = true
			break
		}
	}
	if !found {
		entries = append(entries, entry)
	}
	return WriteFilesManifest(dataDir, collection, docID, entries)
}

// RemoveManifestEntry deletes one entry by name and rewrites the manifest.
func RemoveManifestEntry(dataDir, collection, docID, filename string) error {
	entries, err := ReadFilesManifest(dataDir, collection, docID)
	if err != nil {
		return err
	}
	out := entries[:0]
	for _, e := range entries {
		if e.Name == filename {
			continue
		}
		out = append(out, e)
	}
	return WriteFilesManifest(dataDir, collection, docID, out)
}

// StagedBinary is a streamed upload waiting for commit under the document lock.
type StagedBinary struct {
	StagedPath string
	ByteSize   int64
	SHA256     string
}

// StageWriteBinary streams r into {collection}/.fileOps/{docId}/.stage-* without
// touching the final LFS path. Call CommitStagedBinary under the document lock.
func StageWriteBinary(dataDir, collection, docID, filename string, r io.Reader, maxBytes int64) (StagedBinary, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxFileBytes
	}
	if !SafeFileName(filename) {
		return StagedBinary{}, fmt.Errorf("invalid file name")
	}
	dir := FileOpsDir(dataDir, collection, docID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return StagedBinary{}, err
	}
	tmp, err := os.CreateTemp(dir, ".stage-*")
	if err != nil {
		return StagedBinary{}, err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		_ = tmp.Close()
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	h := sha256.New()
	limited := &countingReader{r: io.TeeReader(r, h), max: maxBytes}
	n, err := io.Copy(tmp, limited)
	if err != nil {
		return StagedBinary{}, err
	}
	if err := tmp.Sync(); err != nil {
		return StagedBinary{}, err
	}
	if err := tmp.Close(); err != nil {
		return StagedBinary{}, err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return StagedBinary{}, err
	}
	cleanup = false
	return StagedBinary{StagedPath: tmpName, ByteSize: n, SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}

// FileOpJournal records an in-flight local attachment mutation for crash recovery.
type FileOpJournal struct {
	Command     string `json:"command"` // create, update, delete
	Filename    string `json:"filename"`
	ContentType string `json:"contentType,omitempty"`
	ByteSize    int64  `json:"byteSize,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
	Version     string `json:"version"`
	OperationID string `json:"operationId"`
	StagedPath  string `json:"stagedPath,omitempty"`
	Phase       string `json:"phase"` // staged | blobCommitted | done
}

func fileOpJournalPath(dataDir, collection, docID, operationID string) string {
	return filepath.Join(FileOpsDir(dataDir, collection, docID), operationID+".json")
}

// WriteFileOpJournal durably records an in-flight file operation.
func WriteFileOpJournal(dataDir, collection, docID string, j FileOpJournal) error {
	if err := os.MkdirAll(FileOpsDir(dataDir, collection, docID), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return WriteFileAtomic(fileOpJournalPath(dataDir, collection, docID, j.OperationID), data, 0o644)
}

// ClearFileOpJournal removes a completed journal entry.
func ClearFileOpJournal(dataDir, collection, docID, operationID string) error {
	path := fileOpJournalPath(dataDir, collection, docID, operationID)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// CommitStagedBinary renames a staged upload into LFS and upserts the manifest.
// It journals phases so RecoverFileOps can finish or roll back after a crash.
func CommitStagedBinary(dataDir, collection, docID string, entry FileEntry, staged StagedBinary) error {
	j := FileOpJournal{
		Command:     "create",
		Filename:    entry.Name,
		ContentType: entry.ContentType,
		ByteSize:    entry.ByteSize,
		SHA256:      entry.SHA256,
		Version:     entry.Version,
		OperationID: entry.OperationID,
		StagedPath:  staged.StagedPath,
		Phase:       "staged",
	}
	if existing, err := ReadFilesManifest(dataDir, collection, docID); err == nil {
		if _, ok := LookupFileEntry(existing, entry.Name); ok {
			j.Command = "update"
		}
	}
	if err := WriteFileOpJournal(dataDir, collection, docID, j); err != nil {
		return err
	}
	lfsDir := LFSDir(dataDir, collection, docID)
	if err := os.MkdirAll(lfsDir, 0o755); err != nil {
		return err
	}
	final := BinaryPath(dataDir, collection, docID, entry.Name)
	if err := os.Rename(staged.StagedPath, final); err != nil {
		return fmt.Errorf("atomic rename to %s: %w", final, err)
	}
	j.Phase = "blobCommitted"
	j.StagedPath = ""
	if err := WriteFileOpJournal(dataDir, collection, docID, j); err != nil {
		return err
	}
	if err := UpsertManifestEntry(dataDir, collection, docID, entry); err != nil {
		return err
	}
	j.Phase = "done"
	_ = WriteFileOpJournal(dataDir, collection, docID, j)
	return ClearFileOpJournal(dataDir, collection, docID, entry.OperationID)
}

// CommitFileDelete removes one attachment and updates the manifest under journal protection.
func CommitFileDelete(dataDir, collection, docID string, entry FileEntry) error {
	j := FileOpJournal{
		Command:     "delete",
		Filename:    entry.Name,
		Version:     entry.Version,
		OperationID: entry.OperationID,
		Phase:       "staged",
	}
	if err := WriteFileOpJournal(dataDir, collection, docID, j); err != nil {
		return err
	}
	if err := RemoveBinary(dataDir, collection, docID, entry.Name); err != nil {
		return err
	}
	j.Phase = "blobCommitted"
	if err := WriteFileOpJournal(dataDir, collection, docID, j); err != nil {
		return err
	}
	if err := RemoveManifestEntry(dataDir, collection, docID, entry.Name); err != nil {
		return err
	}
	return ClearFileOpJournal(dataDir, collection, docID, entry.OperationID)
}

// RecoverFileOps finishes or rolls back interrupted file operations for one document.
func RecoverFileOps(dataDir, collection, docID string) error {
	dir := FileOpsDir(dataDir, collection, docID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") {
			if strings.HasPrefix(name, ".stage-") {
				_ = os.Remove(filepath.Join(dir, name))
			}
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		var j FileOpJournal
		if err := json.Unmarshal(raw, &j); err != nil {
			return err
		}
		switch j.Phase {
		case "done":
			_ = ClearFileOpJournal(dataDir, collection, docID, j.OperationID)
		case "blobCommitted":
			if j.Command == "delete" {
				if err := RemoveManifestEntry(dataDir, collection, docID, j.Filename); err != nil {
					return err
				}
			} else {
				if err := UpsertManifestEntry(dataDir, collection, docID, FileEntry{
					Name:        j.Filename,
					ContentType: j.ContentType,
					ByteSize:    j.ByteSize,
					SHA256:      j.SHA256,
					Version:     j.Version,
					OperationID: j.OperationID,
				}); err != nil {
					return err
				}
			}
			_ = ClearFileOpJournal(dataDir, collection, docID, j.OperationID)
		default: // staged or unknown: roll back
			if j.StagedPath != "" {
				_ = os.Remove(j.StagedPath)
			}
			_ = ClearFileOpJournal(dataDir, collection, docID, j.OperationID)
		}
	}
	return nil
}

// RecoverAllFileOps walks collections and recovers interrupted file operations.
func RecoverAllFileOps(dataDir string) error {
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		opsRoot := filepath.Join(CollectionDir(dataDir, e.Name()), ".fileOps")
		docs, err := os.ReadDir(opsRoot)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		for _, d := range docs {
			if !d.IsDir() {
				continue
			}
			if err := RecoverFileOps(dataDir, e.Name(), d.Name()); err != nil {
				return err
			}
		}
	}
	return nil
}

// IsFileTooLarge reports whether err is the streamed size-limit failure.
func IsFileTooLarge(err error) bool {
	return err != nil && strings.Contains(err.Error(), "fileTooLarge")
}
