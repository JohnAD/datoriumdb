package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/JohnAD/datoriumdb/internal/accesslang"
	"github.com/JohnAD/datoriumdb/internal/config"
	"github.com/JohnAD/datoriumdb/internal/envelope"
	"github.com/JohnAD/datoriumdb/internal/fsstore"
	"github.com/JohnAD/datoriumdb/internal/idgen"
	"github.com/JohnAD/datoriumdb/internal/replication"
	"github.com/JohnAD/datoriumdb/internal/rfc6902"
	"github.com/JohnAD/datoriumdb/internal/shard"
	"github.com/JohnAD/ojson"
)

// IDGenerator creates document IDs and versions.
type IDGenerator interface {
	New() (string, error)
}

// ClockULID is the default ULID-backed generator.
type ClockULID struct{}

func (ClockULID) New() (string, error) { return idgen.New() }

// Engine executes access-language commands against local filesystem storage.
type Engine struct {
	ConfigDir  string
	DataDir    string
	ServerName string
	Cfg        *config.Config
	IDs        IDGenerator

	// Replicator pushes SOT writes to this shard slot's read/proxy members
	// after local commit, per tech-docs/REPLICATION-FAILURE-HANDLING.md.
	// Nil is valid: writes still commit locally and record a durable
	// operation, but nothing is pushed (e.g. single-machine mode, or a
	// server not currently wired for outbound replication calls).
	Replicator *replication.Coordinator

	// ReadState tracks read-member catch-up staleness (per-document and
	// per-SOT-server), per REPLICATION-FAILURE-HANDLING.md's "Read-Member
	// Catch-Up". Nil disables staleness refusal (reads are always served
	// once routing accepts them).
	ReadState *replication.ReadMemberState

	mu sync.Mutex // serializes same-document mutations for version races in-process
}

func (e *Engine) ids() IDGenerator {
	if e.IDs != nil {
		return e.IDs
	}
	return ClockULID{}
}

// Reload loads establishment config from ConfigDir.
func (e *Engine) Reload() error {
	cfg, err := config.Load(e.ConfigDir)
	if err != nil {
		return err
	}
	e.Cfg = cfg
	return nil
}

// Execute parses and runs one access-language command.
func (e *Engine) Execute(line string) envelope.Result {
	cmd, err := accesslang.Parse(line)
	if err != nil {
		return envelope.Fail(map[string]any{"command": "unknown"}, envelope.Error{
			Code:    "invalidCommand",
			Message: err.Error(),
		})
	}
	detail, err := accesslang.ParseDetail(cmd.Detail)
	if err != nil {
		return envelope.Fail(map[string]any{"command": cmd.Word}, envelope.Error{
			Code:    "invalidDetail",
			Message: err.Error(),
		})
	}
	switch cmd.Word {
	case "create":
		return e.create(cmd, detail)
	case "read":
		return e.read(cmd, detail)
	case "patch":
		return e.patch(cmd, detail)
	case "delete":
		return e.delete(cmd, detail)
	case "search":
		return e.search(cmd, detail)
	default:
		return envelope.Fail(map[string]any{"command": cmd.Word}, envelope.Error{
			Code:    "unknownCommand",
			Message: "unsupported command",
		})
	}
}

func (e *Engine) create(cmd accesslang.Command, detail map[string]any) envelope.Result {
	collection := cmd.Target
	opID := stringField(detail, "operationId")
	if opID != "" {
		if cached, ok := e.idempotentReplay(opID); ok {
			return cached
		}
	}
	schemaRaw, ok := e.Cfg.Schemas[collection]
	if !ok {
		return envelope.Fail(map[string]any{"command": "create", "collection": collection}, envelope.Error{
			Code:    "collectionNotFound",
			Message: "collection does not exist",
		})
	}
	id := cmd.Parm
	if id == "null" {
		var err error
		id, err = e.ids().New()
		if err != nil {
			return envelope.Fail(map[string]any{"command": "create", "collection": collection}, envelope.Error{
				Code:    "idGenerationFailed",
				Message: err.Error(),
			})
		}
	}
	if !idgen.ValidDocumentID(id) || !fsstore.SafeID(id) {
		return envelope.Fail(map[string]any{"command": "create", "collection": collection, "id": id}, envelope.Error{
			Code:    "invalidDocumentId",
			Message: "document id is invalid or unsafe for filesystem storage",
		})
	}
	if wrong := e.checkRouting(id, "create", collection); wrong != nil {
		return *wrong
	}
	if opID == "" {
		var err error
		opID, err = e.ids().New()
		if err != nil {
			return envelope.Fail(map[string]any{"command": "create", "collection": collection, "id": id}, envelope.Error{
				Code:    "idGenerationFailed",
				Message: err.Error(),
			})
		}
	}
	marker := currentSchemaMarker(collection, e.Cfg)
	if v, ok := detail["$"].(string); ok && v != "" {
		if v != marker {
			return envelope.Fail(map[string]any{"command": "create", "collection": collection, "id": id}, envelope.Error{
				Code:     "schemaMismatch",
				Path:     "/$",
				Message:  "Document schema marker does not match the collection schema.",
				Expected: marker,
				Actual:   v,
			})
		}
	} else {
		detail["$"] = marker
	}
	if _, hasSharp := detail["#"]; hasSharp {
		return envelope.Fail(map[string]any{"command": "create", "collection": collection, "id": id}, envelope.Error{
			Code:    "invalidMetadata",
			Path:    "/#",
			Message: "create content cannot include #",
		})
	}
	if bang, ok := detail["!"].(string); ok && bang != "" && bang != id {
		return envelope.Fail(map[string]any{"command": "create", "collection": collection, "id": id}, envelope.Error{
			Code:     "idMismatch",
			Path:     "/!",
			Message:  "supplied ! does not match document id",
			Expected: id,
			Actual:   bang,
		})
	}
	detail["!"] = id
	delete(detail, "operationId")
	content := stripWriteMeta(detail)
	if err := validateAgainstSchema(schemaRaw, content); err != nil {
		return envelope.Fail(map[string]any{"command": "create", "collection": collection, "id": id}, envelope.Error{
			Code:    "invalidSchema",
			Message: err.Error(),
		})
	}
	version, err := e.ids().New()
	if err != nil {
		return envelope.Fail(map[string]any{"command": "create", "collection": collection, "id": id}, envelope.Error{
			Code:    "idGenerationFailed",
			Message: err.Error(),
		})
	}
	doc := map[string]any{
		"!": id,
		"$": marker,
		"#": version,
	}
	for k, v := range content {
		doc[k] = v
	}
	path := fsstore.DocumentPath(e.DataDir, collection, id)
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, err := os.Stat(path); err == nil {
		return envelope.Fail(map[string]any{"command": "create", "collection": collection, "id": id}, envelope.Error{
			Code:    "documentExists",
			Message: "document already exists",
		})
	}
	if err := fsstore.EnsureCollectionDir(e.DataDir, collection); err != nil {
		return envelope.Fail(map[string]any{"command": "create", "collection": collection, "id": id}, envelope.Error{
			Code:    "filesystemError",
			Message: err.Error(),
		})
	}
	op, opErr := replication.Begin(e.DataDir, replication.DocumentWorkItem{
		Collection:   collection,
		ID:           id,
		AfterVersion: version,
		OperationID:  opID,
		Command:      "create",
		Payload:      doc,
	})
	if opErr != nil {
		return envelope.Fail(map[string]any{"command": "create", "collection": collection, "id": id}, envelope.Error{
			Code:    "operationTrackingFailed",
			Message: opErr.Error(),
		})
	}
	if err := op.SetState(e.DataDir, replication.StateValidated); err != nil {
		return envelope.Fail(map[string]any{"command": "create", "collection": collection, "id": id}, envelope.Error{
			Code:    "operationTrackingFailed",
			Message: err.Error(),
		})
	}
	if err := fsstore.WriteDocumentJSONVerified(path, doc); err != nil {
		_ = op.SetState(e.DataDir, replication.StateFailed)
		return envelope.Fail(map[string]any{"command": "create", "collection": collection, "id": id}, envelope.Error{
			Code:    "filesystemError",
			Message: err.Error(),
		})
	}
	if err := op.SetState(e.DataDir, replication.StateCommittedLocal); err != nil {
		return envelope.Fail(map[string]any{"command": "create", "collection": collection, "id": id}, envelope.Error{
			Code:    "operationTrackingFailed",
			Message: err.Error(),
		})
	}
	if err := fsstore.EnqueueChange(e.DataDir, collection, id, "create"); err != nil {
		return envelope.Fail(map[string]any{"command": "create", "collection": collection, "id": id}, envelope.Error{
			Code:    "queueWriteFailed",
			Message: err.Error(),
		})
	}
	result := envelope.OK(map[string]any{
		"command":     "create",
		"collection":  collection,
		"id":          id,
		"$":           marker,
		"#":           version,
		"operationId": opID,
	})
	return e.finalizeWrite(op, result)
}

func (e *Engine) read(cmd accesslang.Command, detail map[string]any) envelope.Result {
	collection := cmd.Target
	id := cmd.Parm
	if !idgen.ValidDocumentID(id) || !fsstore.SafeID(id) {
		return envelope.Fail(map[string]any{"command": "read", "collection": collection, "id": id}, envelope.Error{
			Code:    "invalidDocumentId",
			Message: "document id is invalid or unsafe for filesystem storage",
		})
	}
	if wrong := e.checkRouting(id, "read", collection); wrong != nil {
		return *wrong
	}
	if wrong := e.checkStaleness(id, "read", collection); wrong != nil {
		return *wrong
	}
	path := fsstore.DocumentPath(e.DataDir, collection, id)
	doc, err := fsstore.ReadDocumentJSON(path)
	if err != nil {
		return envelope.Fail(map[string]any{"command": "read", "collection": collection, "id": id}, envelope.Error{
			Code:    "documentNotFound",
			Message: "document not found",
		})
	}
	if doc, err = e.migrateOnAccess(collection, id, doc); err != nil {
		return envelope.Fail(map[string]any{"command": "read", "collection": collection, "id": id}, envelope.Error{
			Code:    "schemaMigrationFailed",
			Message: err.Error(),
		})
	}
	schemaRaw, ok := e.Cfg.Schemas[collection]
	sot, extra := splitSOTAndExtra(doc, schemaRaw, ok)
	out := map[string]any{
		"command":    "read",
		"collection": collection,
		"id":         id,
		"sot":        sot,
	}
	if wantExtra, _ := detail["extraFields"].(bool); wantExtra {
		out["extraFields"] = extra
	}
	if wantCache, _ := detail["cacheSummaries"].(bool); wantCache {
		out["cacheSummaries"] = e.buildCacheSummaries(doc, schemaRaw, ok)
	}
	return envelope.OK(out)
}

func (e *Engine) patch(cmd accesslang.Command, detail map[string]any) envelope.Result {
	collection := cmd.Target
	id := cmd.Parm
	opID := stringField(detail, "operationId")
	if opID != "" {
		if cached, ok := e.idempotentReplay(opID); ok {
			return cached
		}
	}
	if !idgen.ValidDocumentID(id) || !fsstore.SafeID(id) {
		return envelope.Fail(map[string]any{"command": "patch", "collection": collection, "id": id}, envelope.Error{
			Code:    "invalidDocumentId",
			Message: "document id is invalid or unsafe for filesystem storage",
		})
	}
	if wrong := e.checkRouting(id, "patch", collection); wrong != nil {
		return *wrong
	}
	schemaRaw, ok := e.Cfg.Schemas[collection]
	if !ok {
		return envelope.Fail(map[string]any{"command": "patch", "collection": collection, "id": id}, envelope.Error{
			Code:    "collectionNotFound",
			Message: "collection does not exist",
		})
	}
	path := fsstore.DocumentPath(e.DataDir, collection, id)
	e.mu.Lock()
	defer e.mu.Unlock()
	doc, err := fsstore.ReadDocumentJSON(path)
	if err != nil {
		return envelope.Fail(map[string]any{"command": "patch", "collection": collection, "id": id}, envelope.Error{
			Code:    "documentNotFound",
			Message: "document not found",
		})
	}
	expectedVer, _ := detail["#"].(string)
	actualVer, _ := doc["#"].(string)
	if expectedVer == "" || expectedVer != actualVer {
		return envelope.Fail(map[string]any{"command": "patch", "collection": collection, "id": id}, envelope.Error{
			Code:     "versionMismatch",
			Path:     "/#",
			Message:  "Document version does not match.",
			Expected: expectedVer,
			Actual:   actualVer,
		})
	}
	marker := currentSchemaMarker(collection, e.Cfg)
	if v, _ := detail["$"].(string); v != marker {
		return envelope.Fail(map[string]any{"command": "patch", "collection": collection, "id": id}, envelope.Error{
			Code:     "schemaMismatch",
			Path:     "/$",
			Message:  "Document schema marker does not match the collection schema.",
			Expected: marker,
			Actual:   v,
		})
	}
	if bang, _ := doc["!"].(string); bang != id {
		return envelope.Fail(map[string]any{"command": "patch", "collection": collection, "id": id}, envelope.Error{
			Code:     "idMismatch",
			Path:     "/!",
			Message:  "document ! does not match id",
			Expected: id,
			Actual:   bang,
		})
	}
	ops, ok := detail["RFC6902"].([]any)
	if !ok {
		return envelope.Fail(map[string]any{"command": "patch", "collection": collection, "id": id}, envelope.Error{
			Code:    "invalidPatch",
			Message: "RFC6902 array is required",
		})
	}
	replicatedOps := make([]map[string]any, 0, len(ops)+1)
	for _, raw := range ops {
		op, _ := raw.(map[string]any)
		p, _ := op["path"].(string)
		from, _ := op["from"].(string)
		if restrictedPointer(p) || restrictedPointer(from) {
			return envelope.Fail(map[string]any{"command": "patch", "collection": collection, "id": id}, envelope.Error{
				Code:    "invalidPatchPath",
				Path:    p,
				Message: "cannot patch database-owned metadata",
			})
		}
		if err := rfc6902.Apply(doc, op); err != nil {
			return envelope.Fail(map[string]any{"command": "patch", "collection": collection, "id": id}, envelope.Error{
				Code:    "invalidPatch",
				Message: err.Error(),
			})
		}
		replicatedOps = append(replicatedOps, op)
	}
	content := stripWriteMeta(doc)
	if err := validateAgainstSchema(schemaRaw, content); err != nil {
		return envelope.Fail(map[string]any{"command": "patch", "collection": collection, "id": id}, envelope.Error{
			Code:    "invalidSchema",
			Message: err.Error(),
		})
	}
	after, err := e.ids().New()
	if err != nil {
		return envelope.Fail(map[string]any{"command": "patch", "collection": collection, "id": id}, envelope.Error{
			Code:    "idGenerationFailed",
			Message: err.Error(),
		})
	}
	doc["#"] = after
	doc["!"] = id
	doc["$"] = marker
	delete(doc, "operationId")
	// Unlike user-submitted patches, SOT-authored replication patches may
	// update database-owned metadata: every read/proxy member must land on
	// the same "/#" version (SERVER-TO-SERVER-API.md).
	replicatedOps = append(replicatedOps, map[string]any{"op": "replace", "path": "/#", "value": after})
	if opID == "" {
		opID, err = e.ids().New()
		if err != nil {
			return envelope.Fail(map[string]any{"command": "patch", "collection": collection, "id": id}, envelope.Error{
				Code:    "idGenerationFailed",
				Message: err.Error(),
			})
		}
	}
	if err := fsstore.PreservePreviousIfAbsent(e.DataDir, collection, id); err != nil {
		return envelope.Fail(map[string]any{"command": "patch", "collection": collection, "id": id}, envelope.Error{
			Code:    "filesystemError",
			Message: err.Error(),
		})
	}
	op, opErr := replication.Begin(e.DataDir, replication.DocumentWorkItem{
		Collection:    collection,
		ID:            id,
		BeforeVersion: expectedVer,
		AfterVersion:  after,
		OperationID:   opID,
		Command:       "patch",
		Patch:         replicatedOps,
	})
	if opErr != nil {
		return envelope.Fail(map[string]any{"command": "patch", "collection": collection, "id": id}, envelope.Error{
			Code:    "operationTrackingFailed",
			Message: opErr.Error(),
		})
	}
	if err := op.SetState(e.DataDir, replication.StateValidated); err != nil {
		return envelope.Fail(map[string]any{"command": "patch", "collection": collection, "id": id}, envelope.Error{
			Code:    "operationTrackingFailed",
			Message: err.Error(),
		})
	}
	if err := fsstore.WriteDocumentJSONVerified(path, doc); err != nil {
		_ = op.SetState(e.DataDir, replication.StateFailed)
		return envelope.Fail(map[string]any{"command": "patch", "collection": collection, "id": id}, envelope.Error{
			Code:    "filesystemError",
			Message: err.Error(),
		})
	}
	if err := op.SetState(e.DataDir, replication.StateCommittedLocal); err != nil {
		return envelope.Fail(map[string]any{"command": "patch", "collection": collection, "id": id}, envelope.Error{
			Code:    "operationTrackingFailed",
			Message: err.Error(),
		})
	}
	if err := fsstore.EnqueueChange(e.DataDir, collection, id, "patch"); err != nil {
		return envelope.Fail(map[string]any{"command": "patch", "collection": collection, "id": id}, envelope.Error{
			Code:    "queueWriteFailed",
			Message: err.Error(),
		})
	}
	result := envelope.OK(map[string]any{
		"command":     "patch",
		"collection":  collection,
		"id":          id,
		"$":           marker,
		"operationId": opID,
		"versions": map[string]any{
			"before": expectedVer,
			"after":  after,
		},
	})
	return e.finalizeWrite(op, result)
}

func (e *Engine) delete(cmd accesslang.Command, detail map[string]any) envelope.Result {
	collection := cmd.Target
	id := cmd.Parm
	opID := stringField(detail, "operationId")
	if opID != "" {
		if cached, ok := e.idempotentReplay(opID); ok {
			return cached
		}
	}
	if !idgen.ValidDocumentID(id) || !fsstore.SafeID(id) {
		return envelope.Fail(map[string]any{"command": "delete", "collection": collection, "id": id}, envelope.Error{
			Code:    "invalidDocumentId",
			Message: "document id is invalid or unsafe for filesystem storage",
		})
	}
	if wrong := e.checkRouting(id, "delete", collection); wrong != nil {
		return *wrong
	}
	path := fsstore.DocumentPath(e.DataDir, collection, id)
	e.mu.Lock()
	defer e.mu.Unlock()
	doc, err := fsstore.ReadDocumentJSON(path)
	if err != nil {
		return envelope.Fail(map[string]any{"command": "delete", "collection": collection, "id": id}, envelope.Error{
			Code:    "documentNotFound",
			Message: "document not found",
		})
	}
	expectedVer, _ := detail["#"].(string)
	actualVer, _ := doc["#"].(string)
	if expectedVer == "" || expectedVer != actualVer {
		return envelope.Fail(map[string]any{"command": "delete", "collection": collection, "id": id}, envelope.Error{
			Code:     "versionMismatch",
			Path:     "/#",
			Message:  "Document version does not match.",
			Expected: expectedVer,
			Actual:   actualVer,
		})
	}
	if opID == "" {
		opID, err = e.ids().New()
		if err != nil {
			return envelope.Fail(map[string]any{"command": "delete", "collection": collection, "id": id}, envelope.Error{
				Code:    "idGenerationFailed",
				Message: err.Error(),
			})
		}
	}
	op, opErr := replication.Begin(e.DataDir, replication.DocumentWorkItem{
		Collection:    collection,
		ID:            id,
		BeforeVersion: expectedVer,
		AfterVersion:  expectedVer,
		OperationID:   opID,
		Command:       "delete",
	})
	if opErr != nil {
		return envelope.Fail(map[string]any{"command": "delete", "collection": collection, "id": id}, envelope.Error{
			Code:    "operationTrackingFailed",
			Message: opErr.Error(),
		})
	}
	if err := op.SetState(e.DataDir, replication.StateValidated); err != nil {
		return envelope.Fail(map[string]any{"command": "delete", "collection": collection, "id": id}, envelope.Error{
			Code:    "operationTrackingFailed",
			Message: err.Error(),
		})
	}
	if err := fsstore.SoftDeleteDocument(e.DataDir, collection, id); err != nil {
		_ = op.SetState(e.DataDir, replication.StateFailed)
		return envelope.Fail(map[string]any{"command": "delete", "collection": collection, "id": id}, envelope.Error{
			Code:    "filesystemError",
			Message: err.Error(),
		})
	}
	if err := op.SetState(e.DataDir, replication.StateCommittedLocal); err != nil {
		return envelope.Fail(map[string]any{"command": "delete", "collection": collection, "id": id}, envelope.Error{
			Code:    "operationTrackingFailed",
			Message: err.Error(),
		})
	}
	if err := fsstore.EnqueueChange(e.DataDir, collection, id, "delete"); err != nil {
		return envelope.Fail(map[string]any{"command": "delete", "collection": collection, "id": id}, envelope.Error{
			Code:    "queueWriteFailed",
			Message: err.Error(),
		})
	}
	result := envelope.OK(map[string]any{
		"command":     "delete",
		"collection":  collection,
		"id":          id,
		"#":           expectedVer,
		"operationId": opID,
	})
	return e.finalizeWrite(op, result)
}

func restrictedPointer(p string) bool {
	if p == "" {
		return false
	}
	return p == "/!" || p == "/$" || p == "/#" ||
		strings.HasPrefix(p, "/!/") || strings.HasPrefix(p, "/$/") || strings.HasPrefix(p, "/#/")
}

// checkRouting implements SHARDING.md's command-aware routing: create,
// patch, and delete only succeed on the shard slot's SHARD_SOT_MEMBER.
// read only succeeds on an assigned SHARD_READ_MEMBER, never on a machine
// that is only a PROXY_READ_MEMBER or otherwise unrelated to the slot
// ("PROXY_READ_MEMBER servers are not normal smart-client read targets",
// SHARDING.md). A dual-role machine (one that is both SOT and read member
// for a slot) naturally passes both checks, which is how "preferring local
// server when it has that role" is realized on the server side: a local
// dual-role server never has to bounce a read to itself.
func (e *Engine) checkRouting(id, command, collection string) *envelope.Result {
	if e.Cfg == nil || e.ServerName == "" {
		return nil
	}
	slot := shard.Slot(id)
	assignment := replication.AssignmentForSlot(e.Cfg, slot)
	switch command {
	case "create", "patch", "delete":
		if assignment.ShardSOTMember == e.ServerName {
			return nil
		}
		return wrongMachineResult(command, collection, id, slot, assignment.ShardSOTMember, e.Cfg,
			"This server is not the SHARD_SOT_MEMBER for the target shard.")
	case "read":
		if containsServer(assignment.ShardReadMember, e.ServerName) {
			return nil
		}
		hint := ""
		if len(assignment.ShardReadMember) > 0 {
			hint = assignment.ShardReadMember[0]
		}
		return wrongMachineResult(command, collection, id, slot, hint, e.Cfg,
			"This server is not a SHARD_READ_MEMBER for the target shard; refresh establishment config and retry.")
	default:
		return nil
	}
}

// checkStaleness implements REPLICATION-FAILURE-HANDLING.md's read-member
// refusal rules: refuse a specific document known to be out of date, and
// refuse all reads for a shard slot once check-ins with its SOT-member have
// failed too many times in a row.
func (e *Engine) checkStaleness(id, command, collection string) *envelope.Result {
	if e.ReadState == nil || e.Cfg == nil {
		return nil
	}
	slot := shard.Slot(id)
	assignment := replication.AssignmentForSlot(e.Cfg, slot)
	sot := assignment.ShardSOTMember
	if sot != "" && sot != e.ServerName && e.ReadState.IsStaleForSOT(sot) {
		res := envelope.Fail(map[string]any{
			"command":    command,
			"collection": collection,
			"id":         id,
		}, envelope.Error{
			Code:    "readMemberStale",
			Message: "This read member has failed too many check-ins with the shard's SOT-member and refuses all reads for this shard slot until it catches up.",
		})
		return &res
	}
	if e.ReadState.IsPending(collection, id) {
		res := envelope.Fail(map[string]any{
			"command":    command,
			"collection": collection,
			"id":         id,
		}, envelope.Error{
			Code:    "documentStale",
			Message: "This document has a pending replicated write that has not been applied yet.",
		})
		return &res
	}
	return nil
}

// wrongMachineResult builds the flat wrongMachine response fields described
// in SHARDING.md and ESTABLISHMENT-CONFIG.md: shardSlot, correctServer,
// baseURL, and configVersion sit directly on the response envelope
// (alongside command/collection/id) rather than nested inside the error,
// so a smart client can read them without digging into errors[0].
func wrongMachineResult(command, collection, id string, slot byte, correctServer string, cfg *config.Config, message string) *envelope.Result {
	baseURL := ""
	if correctServer != "" {
		if s, ok := cfg.Servers.Servers[correctServer]; ok {
			baseURL = s.BaseURL
		}
	}
	res := envelope.Fail(map[string]any{
		"command":       command,
		"collection":    collection,
		"id":            id,
		"shardSlot":     fmt.Sprintf("%02X", slot),
		"correctServer": correctServer,
		"baseURL":       baseURL,
		"configVersion": cfg.General.General.Version,
	}, envelope.Error{
		Code:    "wrongMachine",
		Message: message,
	})
	return &res
}

func containsServer(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// idempotentReplay implements REPLICATION-FAILURE-HANDLING.md's
// idempotency requirement: a retry with the same operationId after a local
// commit already happened must not re-apply the write, and must return the
// exact response the caller would have received the first time.
func (e *Engine) idempotentReplay(operationID string) (envelope.Result, bool) {
	op, err := replication.Load(e.DataDir, operationID)
	if err != nil || op.Response == nil {
		return nil, false
	}
	switch op.State {
	case replication.StateCommittedLocal, replication.StateReplicating, replication.StateReplicated:
		return op.Response, true
	default:
		return nil, false
	}
}

// finalizeWrite runs happy-path replication for a just-committed write
// (SHARDING.md / REPLICATION-FAILURE-HANDLING.md): push to every read and
// proxy member of the shard slot, add a response "note" when one or more
// targets do not acknowledge within the timeout, and durably persist the
// final response so a duplicate client retry by operationId can be
// answered without re-executing the write.
func (e *Engine) finalizeWrite(op *replication.Operation, result envelope.Result) envelope.Result {
	if e.Replicator != nil {
		slot := shard.Slot(op.Item.ID)
		assignment := replication.AssignmentForSlot(e.Cfg, slot)
		targets := e.Replicator.TargetsForAssignment(assignment)
		note, err := e.Replicator.ReplicateOperation(context.Background(), op, targets)
		if err == nil && note != nil {
			result["note"] = note
		}
	} else {
		_ = op.SetState(e.DataDir, replication.StateReplicated)
	}
	op.Response = result
	_ = op.Save(e.DataDir)
	return result
}

func stringField(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

func currentSchemaMarker(collection string, cfg *config.Config) string {
	ver := 0
	if cfg != nil {
		ver = cfg.SchemaVersion(collection)
	}
	return fmt.Sprintf("%s:%d", collection, ver)
}

func stripWriteMeta(detail map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range detail {
		switch k {
		case "operationId", "!", "$", "#", "RFC6902":
			continue
		default:
			out[k] = v
		}
	}
	return out
}

func splitSOTAndExtra(doc map[string]any, schemaRaw json.RawMessage, haveSchema bool) (sot, extra map[string]any) {
	sot = map[string]any{}
	extra = map[string]any{}
	schemaFields := map[string]bool{"!": true, "$": true, "#": true}
	if haveSchema {
		for name := range schemaFieldNames(schemaRaw) {
			schemaFields[name] = true
		}
	}
	for k, v := range doc {
		if schemaFields[k] {
			sot[k] = v
		} else {
			extra[k] = v
		}
	}
	return sot, extra
}

func schemaFieldNames(schemaRaw json.RawMessage) map[string]bool {
	out := map[string]bool{}
	var root struct {
		Children []struct {
			Name string `json:"name"`
		} `json:"children"`
	}
	if err := json.Unmarshal(schemaRaw, &root); err != nil {
		return out
	}
	for _, c := range root.Children {
		if c.Name != "" {
			out[c.Name] = true
		}
	}
	return out
}

func validateAgainstSchema(schemaRaw []byte, doc map[string]any) error {
	schema, err := config.CompileSchemaBytes(schemaRaw)
	if err != nil {
		return err
	}
	if schema.Kind() != ojson.KindObject {
		return fmt.Errorf("collection schema root must be object")
	}
	b, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	value, err := ojson.ReadBytesNoSchema(b)
	if err != nil {
		return err
	}
	return schema.Validate(value)
}
