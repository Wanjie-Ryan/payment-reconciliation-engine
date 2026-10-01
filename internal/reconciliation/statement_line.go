package reconciliation

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// StatementLine is what the payment provider reported for one reference,
// recorded as-is. Comparing it against the internal ledger and producing
// discrepancies is handled by Service.Reconcile - this type just gives a
// provider statement a durable, idempotent home the moment it arrives.
type StatementLine struct {
	id                uuid.UUID
	providerReference string
	amount            int64 // minor units; no currency column on this table
	status            string
	reportedAt        time.Time
}

func NewStatementLine(providerReference string, amount int64, status string, reportedAt time.Time) (*StatementLine, error) {
	if providerReference == "" {
		return nil, fmt.Errorf("%w: provider reference is required", ErrInvalid)
	}
	if status == "" {
		return nil, fmt.Errorf("%w: status is required", ErrInvalid)
	}
	return &StatementLine{
		id:                uuid.New(),
		providerReference: providerReference,
		amount:            amount,
		status:            status,
		reportedAt:        reportedAt,
	}, nil
}

// RehydrateStatementLine rebuilds a StatementLine from data already stored,
// skipping validation - the row was validated when it was first inserted.
func RehydrateStatementLine(id uuid.UUID, providerReference string, amount int64, status string, reportedAt time.Time) *StatementLine {
	return &StatementLine{id: id, providerReference: providerReference, amount: amount, status: status, reportedAt: reportedAt}
}

func (s *StatementLine) ID() uuid.UUID             { return s.id }
func (s *StatementLine) ProviderReference() string { return s.providerReference }
func (s *StatementLine) Amount() int64             { return s.amount }
func (s *StatementLine) Status() string            { return s.status }
func (s *StatementLine) ReportedAt() time.Time     { return s.reportedAt }
