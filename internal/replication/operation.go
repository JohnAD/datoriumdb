// Package replication implements DatoriumDB's durable replication model
// described in tech-docs/REPLICATION-FAILURE-HANDLING.md and
// tech-docs/SERVER-TO-SERVER-API.md: per-operation durable state on the
// SHARD_SOT_MEMBER, idempotent client retry by operationId, happy-path push
// delivery to read/proxy members, `.pendingWrites` fallback on timeout, and
// read-member check-in / catch-up / staleness tracking.
package replication

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/JohnAD/datoriumdb/internal/envelope"
	"github.com/JohnAD/datoriumdb/internal/fsstore"
)

// State is a durable per-operation state, per
// REPLICATION-FAILURE-HANDLING.md's "Required Operation State".
type State string

const (
	StateReceived       State = "received"
	StateValidated      State = "validated"
	StateCommittedLocal State = "committedLocal"
	StateReplicating    State = "replicating"
	StateReplicated     State = "replicated"
	StateFailed         State = "failed"
)

// Terminal reports whether state needs no further SOT-restart recovery
// work. StateFailed is terminal because DatoriumDB only reaches it when the
// local write itself failed, before the operation was accepted; there is
// nothing to resume. StateReplicated is terminal because every required
// target has acknowledged. StateReceived, StateValidated, StateCommittedLocal,
// and StateReplicating are all non-terminal: a crash in any of those means
// replication delivery may still be outstanding and must be resumed.
func (s State) Terminal() bool {
	switch s {
	case StateReplicated, StateFailed:
		return true
	default:
		return false
	}
}

// Operation is the durable per-operation record kept by an SOT-member. Item
// carries the full replicated-write body (collection, document ID,
// versions, command, and patch/payload) so a crashed-and-restarted
// SOT-member can resume replication without re-deriving it, and so a
// duplicate client retry by operationId can be answered from Response
// without re-executing the write.
type Operation struct {
	OperationID  string           `json:"operationId"`
	Item         DocumentWorkItem `json:"item"`
	State        State            `json:"state"`
	Targets      []string         `json:"targets,omitempty"`
	Acknowledged []string         `json:"acknowledged,omitempty"`
	Response     envelope.Result  `json:"response,omitempty"`
	CreatedAt    time.Time        `json:"createdAt"`
	UpdatedAt    time.Time        `json:"updatedAt"`
}

// Path returns the durable operation record path for operationID.
func Path(dataDir, operationID string) string {
	return fsstore.OperationPath(dataDir, operationID)
}

// Begin creates and durably persists a new operation record in the
// "received" state for item.
func Begin(dataDir string, item DocumentWorkItem) (*Operation, error) {
	if item.OperationID == "" {
		return nil, fmt.Errorf("replication: operationId is required")
	}
	op := &Operation{
		OperationID: item.OperationID,
		Item:        item,
		State:       StateReceived,
		CreatedAt:   time.Now().UTC(),
	}
	if err := op.Save(dataDir); err != nil {
		return nil, err
	}
	return op, nil
}

// Load reads a durable operation record, if any exists.
func Load(dataDir, operationID string) (*Operation, error) {
	data, err := os.ReadFile(Path(dataDir, operationID))
	if err != nil {
		return nil, err
	}
	var op Operation
	if err := json.Unmarshal(data, &op); err != nil {
		return nil, fmt.Errorf("replication: invalid operation record %s: %w", operationID, err)
	}
	return &op, nil
}

// Save atomically (re)persists op, stamping UpdatedAt.
func (op *Operation) Save(dataDir string) error {
	op.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(op, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return fsstore.WriteFileAtomic(Path(dataDir, op.OperationID), data, 0o644)
}

// SetState transitions op to state and persists the record.
func (op *Operation) SetState(dataDir string, state State) error {
	op.State = state
	return op.Save(dataDir)
}

// ListIncomplete scans the operations directory for every non-terminal
// operation, for SOT-restart recovery per REPLICATION-FAILURE-HANDLING.md:
// "After a crash or restart, the SOT-member scans incomplete operations and
// resumes replication until the operation reaches a terminal state."
func ListIncomplete(dataDir string) ([]*Operation, error) {
	dir := fsstore.OperationsDir(dataDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []*Operation
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		operationID := strings.TrimSuffix(e.Name(), ".json")
		op, err := Load(dataDir, operationID)
		if err != nil {
			continue
		}
		if !op.State.Terminal() {
			out = append(out, op)
		}
	}
	return out, nil
}
