package replication

import (
	"encoding/json"
	"fmt"

	"github.com/JohnAD/datoriumdb/internal/fsstore"
	"github.com/JohnAD/ojson"
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
	if len(item.Payload) == 0 {
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
	doc, err := fsstore.ReadDocumentValue(path)
	if err != nil {
		return false, fmt.Errorf("replication: patch target %s/%s not found locally: %w", item.Collection, item.ID, err)
	}
	if ver := doc.Get("#").ToStringOrEmpty(); ver == item.AfterVersion {
		return true, nil // already applied
	}
	if err := fsstore.PreservePreviousIfAbsent(a.DataDir, item.Collection, item.ID); err != nil {
		return false, err
	}
	// Prefer the SOT's ordered full-document payload so READ storage does
	// not rebuild JSON through unordered Go maps.
	if len(item.Payload) > 0 {
		if err := fsstore.WriteDocumentJSONVerified(path, item.Payload); err != nil {
			return false, err
		}
		return true, nil
	}
	raw, err := json.Marshal(item.Patch)
	if err != nil {
		return false, fmt.Errorf("replication: encode patch for %s/%s: %w", item.Collection, item.ID, err)
	}
	patch, err := ojson.ReadPatchBytes(raw)
	if err != nil {
		return false, fmt.Errorf("replication: parse patch for %s/%s: %w", item.Collection, item.ID, err)
	}
	patched, err := ojson.ApplyPatch(doc, patch)
	if err != nil {
		return false, fmt.Errorf("replication: apply patch op to %s/%s: %w", item.Collection, item.ID, err)
	}
	pretty := patched.ToPrettyJSONBytes(2)
	if len(pretty) == 0 || pretty[len(pretty)-1] != '\n' {
		pretty = append(pretty, '\n')
	}
	if err := fsstore.WriteDocumentJSONVerified(path, pretty); err != nil {
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
