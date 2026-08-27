package engine

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/JohnAD/datoriumdb/internal/envelope"
	"github.com/JohnAD/datoriumdb/internal/fsstore"
	"github.com/JohnAD/datoriumdb/internal/idgen"
	"github.com/JohnAD/datoriumdb/internal/replication"
	"github.com/JohnAD/datoriumdb/internal/shard"
)

// PutFileOptions configures a binary attachment create/update.
type PutFileOptions struct {
	ContentType string
	// IfMatch is the current file version required for update. Empty means create-only.
	IfMatch string
	// OperationID optionally supplies a client-chosen operation id.
	OperationID string
}

// FileWriteFields are the non-error response fields for fileCreate/fileUpdate/fileDelete.
type FileWriteFields struct {
	Command              string
	Collection           string
	ID                   string
	Filename             string
	Version              string
	ByteSize             int64
	SHA256               string
	ContentType          string
	OperationID          string
	DistributionComplete bool
	Note                 any
}

// PutFile creates or updates one attachment for an existing parent document.
// The upload is staged outside the document lock, then committed under it.
func (e *Engine) PutFile(r io.Reader, collection, docID, filename string, opts PutFileOptions) envelope.Result {
	if !idgen.ValidDocumentID(docID) || !fsstore.SafeID(docID) {
		return envelope.Fail(map[string]any{"command": "fileCreate", "collection": collection, "id": docID, "filename": filename}, envelope.Error{
			Code:    "invalidDocumentId",
			Message: "document id is invalid or unsafe for filesystem storage",
		})
	}
	if !fsstore.SafeFileName(filename) {
		return envelope.Fail(map[string]any{"command": "fileCreate", "collection": collection, "id": docID, "filename": filename}, envelope.Error{
			Code:    "invalidFileName",
			Message: "file name is invalid or unsafe for filesystem storage",
		})
	}
	cmd := "fileCreate"
	if opts.IfMatch != "" {
		cmd = "fileUpdate"
	}
	if wrong := e.checkRouting(docID, "create", collection); wrong != nil {
		return *wrongMachineFile(*wrong, cmd, filename)
	}
	maxBytes := e.maxFileBytes()
	staged, err := fsstore.StageWriteBinary(e.DataDir, collection, docID, filename, r, maxBytes)
	if err != nil {
		if fsstore.IsFileTooLarge(err) {
			return envelope.Fail(map[string]any{"command": cmd, "collection": collection, "id": docID, "filename": filename}, envelope.Error{
				Code:    "fileTooLarge",
				Message: fmt.Sprintf("file exceeds maxFileBytes limit of %d", maxBytes),
			})
		}
		return envelope.Fail(map[string]any{"command": cmd, "collection": collection, "id": docID, "filename": filename}, envelope.Error{
			Code:    "filesystemError",
			Message: err.Error(),
		})
	}
	defer func() { _ = os.Remove(staged.StagedPath) }()

	e.mu.Lock()
	localDone := false
	defer func() {
		if !localDone {
			e.mu.Unlock()
		}
	}()

	if !fsstore.DocumentExistsLive(e.DataDir, collection, docID) {
		return envelope.Fail(map[string]any{"command": cmd, "collection": collection, "id": docID, "filename": filename}, envelope.Error{
			Code:    "documentNotFound",
			Message: "parent document not found",
		})
	}
	entries, err := fsstore.ReadFilesManifest(e.DataDir, collection, docID)
	if err != nil {
		return envelope.Fail(map[string]any{"command": cmd, "collection": collection, "id": docID, "filename": filename}, envelope.Error{
			Code:    "filesystemError",
			Message: err.Error(),
		})
	}
	existing, exists := fsstore.LookupFileEntry(entries, filename)
	if opts.IfMatch == "" {
		if exists {
			return envelope.Fail(map[string]any{"command": "fileCreate", "collection": collection, "id": docID, "filename": filename}, envelope.Error{
				Code:    "fileExists",
				Message: "file already exists; provide If-Match with the current file version to update",
				Actual:  existing.Version,
			})
		}
		cmd = "fileCreate"
	} else {
		if !exists {
			return envelope.Fail(map[string]any{"command": "fileUpdate", "collection": collection, "id": docID, "filename": filename}, envelope.Error{
				Code:    "fileNotFound",
				Message: "file not found",
			})
		}
		if existing.Version != opts.IfMatch {
			return envelope.Fail(map[string]any{"command": "fileUpdate", "collection": collection, "id": docID, "filename": filename}, envelope.Error{
				Code:     "fileVersionMismatch",
				Message:  "file version does not match",
				Expected: opts.IfMatch,
				Actual:   existing.Version,
			})
		}
		cmd = "fileUpdate"
	}

	opID := opts.OperationID
	if opID == "" {
		opID, err = e.ids().New()
		if err != nil {
			return envelope.Fail(map[string]any{"command": cmd, "collection": collection, "id": docID, "filename": filename}, envelope.Error{
				Code:    "idGenerationFailed",
				Message: err.Error(),
			})
		}
	}
	version, err := e.ids().New()
	if err != nil {
		return envelope.Fail(map[string]any{"command": cmd, "collection": collection, "id": docID, "filename": filename}, envelope.Error{
			Code:    "idGenerationFailed",
			Message: err.Error(),
		})
	}
	contentType := opts.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	entry := fsstore.FileEntry{
		Name:        filename,
		ContentType: contentType,
		ByteSize:    staged.ByteSize,
		SHA256:      staged.SHA256,
		Version:     version,
		OperationID: opID,
	}
	if err := fsstore.CommitStagedBinary(e.DataDir, collection, docID, entry, staged); err != nil {
		return envelope.Fail(map[string]any{"command": cmd, "collection": collection, "id": docID, "filename": filename}, envelope.Error{
			Code:    "filesystemError",
			Message: err.Error(),
		})
	}
	staged.StagedPath = "" // committed; defer remove is no-op
	localDone = true
	e.mu.Unlock()

	item := replication.FileWorkItem{
		Collection:  collection,
		ID:          docID,
		Filename:    filename,
		ContentType: contentType,
		ByteSize:    entry.ByteSize,
		SHA256:      entry.SHA256,
		Version:     version,
		OperationID: opID,
		Command:     cmd,
	}
	result := envelope.OK(map[string]any{
		"command":     cmd,
		"collection":  collection,
		"id":          docID,
		"filename":    filename,
		"version":     version,
		"byteSize":    entry.ByteSize,
		"sha256":      entry.SHA256,
		"contentType": contentType,
		"operationId": opID,
	})
	return e.deliverFileOnce(item, result)
}

// DeleteFile version-checks and deletes one attachment.
func (e *Engine) DeleteFile(collection, docID, filename, ifMatch, operationID string) envelope.Result {
	if !idgen.ValidDocumentID(docID) || !fsstore.SafeID(docID) {
		return envelope.Fail(map[string]any{"command": "fileDelete", "collection": collection, "id": docID, "filename": filename}, envelope.Error{
			Code:    "invalidDocumentId",
			Message: "document id is invalid or unsafe for filesystem storage",
		})
	}
	if !fsstore.SafeFileName(filename) {
		return envelope.Fail(map[string]any{"command": "fileDelete", "collection": collection, "id": docID, "filename": filename}, envelope.Error{
			Code:    "invalidFileName",
			Message: "file name is invalid or unsafe for filesystem storage",
		})
	}
	if wrong := e.checkRouting(docID, "delete", collection); wrong != nil {
		return *wrongMachineFile(*wrong, "fileDelete", filename)
	}
	e.mu.Lock()
	localDone := false
	defer func() {
		if !localDone {
			e.mu.Unlock()
		}
	}()
	if !fsstore.DocumentExistsLive(e.DataDir, collection, docID) {
		return envelope.Fail(map[string]any{"command": "fileDelete", "collection": collection, "id": docID, "filename": filename}, envelope.Error{
			Code:    "documentNotFound",
			Message: "parent document not found",
		})
	}
	entries, err := fsstore.ReadFilesManifest(e.DataDir, collection, docID)
	if err != nil {
		return envelope.Fail(map[string]any{"command": "fileDelete", "collection": collection, "id": docID, "filename": filename}, envelope.Error{
			Code:    "filesystemError",
			Message: err.Error(),
		})
	}
	existing, ok := fsstore.LookupFileEntry(entries, filename)
	if !ok {
		return envelope.Fail(map[string]any{"command": "fileDelete", "collection": collection, "id": docID, "filename": filename}, envelope.Error{
			Code:    "fileNotFound",
			Message: "file not found",
		})
	}
	if ifMatch == "" || ifMatch != existing.Version {
		return envelope.Fail(map[string]any{"command": "fileDelete", "collection": collection, "id": docID, "filename": filename}, envelope.Error{
			Code:     "fileVersionMismatch",
			Message:  "file version does not match",
			Expected: ifMatch,
			Actual:   existing.Version,
		})
	}
	opID := operationID
	if opID == "" {
		opID, err = e.ids().New()
		if err != nil {
			return envelope.Fail(map[string]any{"command": "fileDelete", "collection": collection, "id": docID, "filename": filename}, envelope.Error{
				Code:    "idGenerationFailed",
				Message: err.Error(),
			})
		}
	}
	entry := existing
	entry.OperationID = opID
	if err := fsstore.CommitFileDelete(e.DataDir, collection, docID, entry); err != nil {
		return envelope.Fail(map[string]any{"command": "fileDelete", "collection": collection, "id": docID, "filename": filename}, envelope.Error{
			Code:    "filesystemError",
			Message: err.Error(),
		})
	}
	localDone = true
	e.mu.Unlock()

	item := replication.FileWorkItem{
		Collection:  collection,
		ID:          docID,
		Filename:    filename,
		Version:     existing.Version,
		OperationID: opID,
		Command:     "fileDelete",
		SHA256:      existing.SHA256,
		ByteSize:    existing.ByteSize,
		ContentType: existing.ContentType,
	}
	result := envelope.OK(map[string]any{
		"command":     "fileDelete",
		"collection":  collection,
		"id":          docID,
		"filename":    filename,
		"version":     existing.Version,
		"byteSize":    existing.ByteSize,
		"sha256":      existing.SHA256,
		"contentType": existing.ContentType,
		"operationId": opID,
	})
	return e.deliverFileOnce(item, result)
}

// ListFiles returns current manifest entries for a document (no binary content).
func (e *Engine) ListFiles(collection, docID string) envelope.Result {
	if !idgen.ValidDocumentID(docID) || !fsstore.SafeID(docID) {
		return envelope.Fail(map[string]any{"command": "fileList", "collection": collection, "id": docID}, envelope.Error{
			Code:    "invalidDocumentId",
			Message: "document id is invalid or unsafe for filesystem storage",
		})
	}
	if wrong := e.checkRouting(docID, "read", collection); wrong != nil {
		return *wrongMachineFile(*wrong, "fileList", "")
	}
	if stale := e.checkFileStaleness(docID, "fileList", collection, ""); stale != nil {
		return *stale
	}
	if !fsstore.DocumentExistsLive(e.DataDir, collection, docID) {
		return envelope.Fail(map[string]any{"command": "fileList", "collection": collection, "id": docID}, envelope.Error{
			Code:    "documentNotFound",
			Message: "parent document not found",
		})
	}
	entries, err := fsstore.ReadFilesManifest(e.DataDir, collection, docID)
	if err != nil {
		return envelope.Fail(map[string]any{"command": "fileList", "collection": collection, "id": docID}, envelope.Error{
			Code:    "filesystemError",
			Message: err.Error(),
		})
	}
	files := make([]map[string]any, 0, len(entries))
	for _, ent := range entries {
		files = append(files, map[string]any{
			"name":        ent.Name,
			"contentType": ent.ContentType,
			"byteSize":    ent.ByteSize,
			"sha256":      ent.SHA256,
			"version":     ent.Version,
			"operationId": ent.OperationID,
		})
	}
	return envelope.OK(map[string]any{
		"command":    "fileList",
		"collection": collection,
		"id":         docID,
		"files":      files,
	})
}

// OpenFileForDownload validates routing/staleness and opens the binary for streaming.
func (e *Engine) OpenFileForDownload(collection, docID, filename string) (entry fsstore.FileEntry, f *os.File, errRes *envelope.Result) {
	fail := func(code, msg string) (*os.File, *envelope.Result) {
		res := envelope.Fail(map[string]any{"command": "fileRead", "collection": collection, "id": docID, "filename": filename}, envelope.Error{
			Code:    code,
			Message: msg,
		})
		return nil, &res
	}
	if !idgen.ValidDocumentID(docID) || !fsstore.SafeID(docID) {
		_, r := fail("invalidDocumentId", "document id is invalid or unsafe for filesystem storage")
		return fsstore.FileEntry{}, nil, r
	}
	if !fsstore.SafeFileName(filename) {
		_, r := fail("invalidFileName", "file name is invalid or unsafe for filesystem storage")
		return fsstore.FileEntry{}, nil, r
	}
	if wrong := e.checkRouting(docID, "read", collection); wrong != nil {
		return fsstore.FileEntry{}, nil, wrongMachineFile(*wrong, "fileRead", filename)
	}
	if stale := e.checkFileStaleness(docID, "fileRead", collection, filename); stale != nil {
		return fsstore.FileEntry{}, nil, stale
	}
	if !fsstore.DocumentExistsLive(e.DataDir, collection, docID) {
		_, r := fail("documentNotFound", "parent document not found")
		return fsstore.FileEntry{}, nil, r
	}
	entries, err := fsstore.ReadFilesManifest(e.DataDir, collection, docID)
	if err != nil {
		_, r := fail("filesystemError", err.Error())
		return fsstore.FileEntry{}, nil, r
	}
	ent, ok := fsstore.LookupFileEntry(entries, filename)
	if !ok {
		_, r := fail("fileNotFound", "file not found")
		return fsstore.FileEntry{}, nil, r
	}
	f, err = fsstore.OpenBinary(e.DataDir, collection, docID, filename)
	if err != nil {
		if os.IsNotExist(err) {
			_, r := fail("fileNotFound", "file not found")
			return fsstore.FileEntry{}, nil, r
		}
		_, r := fail("filesystemError", err.Error())
		return fsstore.FileEntry{}, nil, r
	}
	return ent, f, nil
}

func (e *Engine) maxFileBytes() int64 {
	if e.Cfg != nil && e.Cfg.General.General.MaxFileBytes > 0 {
		return e.Cfg.General.General.MaxFileBytes
	}
	return fsstore.DefaultMaxFileBytes
}

func (e *Engine) deliverFileOnce(item replication.FileWorkItem, result envelope.Result) envelope.Result {
	complete := true
	if e.Replicator != nil {
		slot := shard.Slot(item.ID)
		assignment := replication.AssignmentForSlot(e.Cfg, slot)
		targets := e.Replicator.TargetsForAssignment(assignment)
		outcome := e.Replicator.DeliverFileOnce(context.Background(), item, targets)
		complete = outcome.Complete()
		if !complete {
			result["note"] = replication.BuildFileNote(outcome)
		}
	}
	result["distributionComplete"] = complete
	return result
}

func (e *Engine) checkFileStaleness(id, command, collection, filename string) *envelope.Result {
	if e.ReadState == nil || e.Cfg == nil {
		return nil
	}
	slot := shard.Slot(id)
	assignment := replication.AssignmentForSlot(e.Cfg, slot)
	sot := assignment.ShardSOTMember
	if sot != "" && sot != e.ServerName && e.ReadState.IsStaleForSOT(sot) {
		fields := map[string]any{"command": command, "collection": collection, "id": id}
		if filename != "" {
			fields["filename"] = filename
		}
		res := envelope.Fail(fields, envelope.Error{
			Code:    "fileStale",
			Message: "This read member has failed too many check-ins with the shard's SOT-member and refuses file reads for this shard slot until it catches up.",
		})
		return &res
	}
	if e.ReadState.IsPendingFile(collection, id, filename) || (filename == "" && e.ReadState.HasPendingFilesForDoc(collection, id)) {
		fields := map[string]any{"command": command, "collection": collection, "id": id}
		if filename != "" {
			fields["filename"] = filename
		}
		res := envelope.Fail(fields, envelope.Error{
			Code:    "fileStale",
			Message: "This file has a pending replicated write that has not been applied yet.",
		})
		return &res
	}
	return nil
}

func wrongMachineFile(base envelope.Result, command, filename string) *envelope.Result {
	if command != "" {
		base["command"] = command
	}
	if filename != "" {
		base["filename"] = filename
	}
	return &base
}
