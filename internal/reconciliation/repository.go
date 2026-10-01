package reconciliation

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// StatementLineRepository is defined by this domain and implemented by
// internal/postgres - same dependency-inversion pattern as the ledger
// context's repositories.
type StatementLineRepository interface {
	// Save is idempotent on provider reference: a duplicate delivery of the
	// same reference is a no-op, not an error.
	Save(ctx context.Context, line *StatementLine) error
	// FindByProviderReference returns an error wrapping
	// ErrStatementLineNotFound if no line has been recorded for that
	// reference yet.
	FindByProviderReference(ctx context.Context, reference string) (*StatementLine, error)
}

// DiscrepancyRepository stores discrepancies found during reconciliation.
type DiscrepancyRepository interface {
	// Save is idempotent per (transaction, discrepancy type): re-running
	// Reconcile does not create a duplicate unresolved discrepancy for the
	// same already-flagged problem.
	Save(ctx context.Context, d *Discrepancy) error
}

// PendingTransaction is the minimal view reconciliation needs of a ledger
// transaction - deliberately not ledger.Transaction, keeping this bounded
// context decoupled from the ledger domain's own types.
type PendingTransaction struct {
	ID             uuid.UUID
	IdempotencyKey string
	Amount         int64
	CreatedAt      time.Time
}

// TransactionReader and TransactionWriter let reconciliation see and act on
// ledger transactions without importing the ledger package - implemented by
// the postgres adapter, same dependency-inversion pattern as every other
// repository in this project.
type TransactionReader interface {
	// FindPendingDeposits returns deposits still "pending" and created at
	// or before cutoff - the grace window before a missing webhook counts
	// as a discrepancy rather than "still in flight."
	FindPendingDeposits(ctx context.Context, cutoff time.Time) ([]PendingTransaction, error)
}

type TransactionWriter interface {
	MarkSettled(ctx context.Context, transactionID uuid.UUID) error
	MarkFailed(ctx context.Context, transactionID uuid.UUID) error
}
