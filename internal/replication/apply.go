package replication

import (
	"fmt"

	"github.com/JohnAD/datoriumdb/internal/fsstore"
	"github.com/JohnAD/datoriumdb/internal/rfc6902"
)

// Applier applies replicated document write work items to local
// shard-local storage on a SHARD_READ_MEMBER or PROXY_READ_MEMBER, per
// SHARDING.md's "MVP Shard-Local Storage Model" and
// REPLICATION-FAILURE-HANDLING.md. Application is idempotent: delivering
// the same item twice (happy-path push retried, or a pending write applied
// after the push already landed) must not double-apply.
type Applier struct {
	DataDir string
}

// Apply applies item idempotently. applied is true when item's
// AfterVersion is durably reflected locally afterward, whether this call
// did the work or a previous delivery already did.
func (a *Applier) Apply(item DocumentWorkItem) (applied bool, err error) {
	switch item.Command {
	case "create":
		return a.applyCreate(item)
	case "patch":
		return a.applyPatch(item)
	case "delete":
		return a.applyDelete(item)
	default:
		return false, fmt.Errorf("replication: unknown command %q", item.Command)
	}
}

func (a *Applier) applyCreate(item DocumentWorkItem) (bool, error) {
	path := fsstore.DocumentPath(a.DataDir, item.Collection, item.ID)
	if existing, err := fsstore.ReadDocumentJSON(path); err == nil {
		if ver, _ := existing["#"].(string); ver == item.AfterVersion {
			return true, nil // already applied
		}
		return false, fmt.Errorf("replication: create conflict for %s/%s: local version differs from replicated version", item.Collection, item.ID)
	}
	if item.Payload == nil {
		return false, fmt.Errorf("replication: create work item for %s/%s is missing payload", item.Collection, item.ID)
	}
	if err := fsstore.EnsureCollectionDir(a.DataDir, item.Collection); err != nil {
		return false, err
	}
	if err := fsstore.WriteDocumentJSONVerified(path, item.Payload); err != nil {
		return false, err
	}
	return true, nil
}

func (a *Applier) applyPatch(item DocumentWorkItem) (bool, error) {
	path := fsstore.DocumentPath(a.DataDir, item.Collection, item.ID)
	doc, err := fsstore.ReadDocumentJSON(path)
	if err != nil {
		return false, fmt.Errorf("replication: patch target %s/%s not found locally: %w", item.Collection, item.ID, err)
	}
	if ver, _ := doc["#"].(string); ver == item.AfterVersion {
		return true, nil // already applied
	}
	if err := fsstore.PreservePreviousIfAbsent(a.DataDir, item.Collection, item.ID); err != nil {
		return false, err
	}
	for _, op := range item.Patch {
		if err := rfc6902.Apply(doc, op); err != nil {
			return false, fmt.Errorf("replication: apply patch op to %s/%s: %w", item.Collection, item.ID, err)
		}
	}
	if err := fsstore.WriteDocumentJSONVerified(path, doc); err != nil {
		return false, err
	}
	return true, nil
}

func (a *Applier) applyDelete(item DocumentWorkItem) (bool, error) {
	path := fsstore.DocumentPath(a.DataDir, item.Collection, item.ID)
	if _, err := fsstore.ReadDocumentJSON(path); err != nil {
		return true, nil // already applied (already gone)
	}
	if err := fsstore.SoftDeleteDocument(a.DataDir, item.Collection, item.ID); err != nil {
		return false, err
	}
	return true, nil
}
