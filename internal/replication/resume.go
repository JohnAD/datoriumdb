package replication

import (
	"context"

	"github.com/JohnAD/datoriumdb/internal/shard"
)

// ReplicateOperation runs the happy-path push for op against allTargets
// (the full required read+proxy member set for op's shard slot), updates
// op's durable state, and returns a REPLICATION-FAILURE-HANDLING.md "note"
// object when one or more targets still have not acknowledged after this
// attempt. Targets already recorded as acknowledged (from an earlier,
// possibly crash-interrupted attempt) are not re-pushed.
func (c *Coordinator) ReplicateOperation(ctx context.Context, op *Operation, allTargets []string) (note map[string]any, err error) {
	op.Targets = allTargets
	if err := op.SetState(c.DataDir, StateReplicating); err != nil {
		return nil, err
	}

	already := map[string]bool{}
	for _, a := range op.Acknowledged {
		already[a] = true
	}
	var remaining []string
	for _, t := range allTargets {
		if !already[t] {
			remaining = append(remaining, t)
		}
	}
	if len(remaining) == 0 {
		if err := op.SetState(c.DataDir, StateReplicated); err != nil {
			return nil, err
		}
		return nil, nil
	}

	outcome := c.ReplicateDocumentWrite(ctx, op.Item, remaining)
	op.Acknowledged = append(op.Acknowledged, outcome.Acknowledged...)
	if len(outcome.Unacknowledged) == 0 {
		if err := op.SetState(c.DataDir, StateReplicated); err != nil {
			return nil, err
		}
		return nil, nil
	}
	if err := op.Save(c.DataDir); err != nil {
		return nil, err
	}
	full := PushOutcome{
		Required:       allTargets,
		Acknowledged:   op.Acknowledged,
		Unacknowledged: outcome.Unacknowledged,
		TimeoutMs:      outcome.TimeoutMs,
	}
	return BuildNote(full), nil
}

// ResumeIncomplete finds every non-terminal durable operation under
// c.DataDir and resumes replication for it, per
// REPLICATION-FAILURE-HANDLING.md: "After a crash or restart, the
// SOT-member scans incomplete operations and resumes replication until the
// operation reaches a terminal state." Local SOT storage is never redone
// here: every non-terminal operation already reached local durable
// commit (or failed before persisting an operation record at all), so only
// replication delivery is retried.
func (c *Coordinator) ResumeIncomplete(ctx context.Context) ([]*Operation, error) {
	ops, err := ListIncomplete(c.DataDir)
	if err != nil {
		return nil, err
	}
	var firstErr error
	var resumed []*Operation
	for _, op := range ops {
		targets := op.Targets
		if targets == nil {
			assignment := AssignmentForSlot(c.Cfg, shard.Slot(op.Item.ID))
			targets = c.TargetsForAssignment(assignment)
		}
		if _, err := c.ReplicateOperation(ctx, op, targets); err != nil && firstErr == nil {
			firstErr = err
		}
		resumed = append(resumed, op)
	}
	return resumed, firstErr
}
