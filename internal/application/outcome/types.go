// Package outcome owns the shared per-target result vocabulary used by
// mutation commands. Command-specific item records remain in their owners.
package outcome

// ItemStatus marks the outcome of one per-target command record.
type ItemStatus string

const (
	// StatusPlanned marks a dry-run or not-yet-executed record.
	StatusPlanned ItemStatus = "planned"
	// StatusCompleted marks a durable record with an established baseline.
	StatusCompleted ItemStatus = "completed"
	// StatusPartial marks a record kept without an equal baseline.
	StatusPartial ItemStatus = "partial"
)

// Summary counts the per-target outcome records of one command.
type Summary struct {
	Planned   int
	Completed int
	Partial   int
}
