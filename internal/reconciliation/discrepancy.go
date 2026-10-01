package reconciliation

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Discrepancy flags a mismatch found while reconciling the internal ledger
// against the provider's statement - something a human needs to look at.
type Discrepancy struct {
	id                uuid.UUID
	transactionID     uuid.UUID
	providerReference string
	discrepancyType   string
	detectedAt        time.Time
	resolved          bool
}

func NewDiscrepancy(transactionID uuid.UUID, providerReference, discrepancyType string) (*Discrepancy, error) {
	if discrepancyType == "" {
		return nil, fmt.Errorf("%w: discrepancy type is required", ErrInvalid)
	}
	return &Discrepancy{
		id:                uuid.New(),
		transactionID:     transactionID,
		providerReference: providerReference,
		discrepancyType:   discrepancyType,
		detectedAt:        time.Now().UTC(),
		resolved:          false,
	}, nil
}

func (d *Discrepancy) ID() uuid.UUID             { return d.id }
func (d *Discrepancy) TransactionID() uuid.UUID  { return d.transactionID }
func (d *Discrepancy) ProviderReference() string { return d.providerReference }
func (d *Discrepancy) DiscrepancyType() string   { return d.discrepancyType }
func (d *Discrepancy) DetectedAt() time.Time     { return d.detectedAt }
func (d *Discrepancy) Resolved() bool            { return d.resolved }
