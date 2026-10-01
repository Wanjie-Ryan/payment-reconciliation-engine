package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sirupsen/logrus"

	"github.com/Wanjie-Ryan/payment-reconciliation-engine/internal/reconciliation"
)

// DiscrepancyRepository implements reconciliation.DiscrepancyRepository.
type DiscrepancyRepository struct {
	pool *pgxpool.Pool
}

func NewDiscrepancyRepository(pool *pgxpool.Pool) *DiscrepancyRepository {
	return &DiscrepancyRepository{pool: pool}
}

// Save inserts the discrepancy unless an unresolved discrepancy of the same
// type already exists for this transaction - this is what makes re-running
// Reconcile safe: it won't pile up duplicate rows for a problem that's
// already been flagged and is still waiting on a human.
func (r *DiscrepancyRepository) Save(ctx context.Context, d *reconciliation.Discrepancy) error {
	fields := logrus.Fields{"transaction_id": d.TransactionID(), "discrepancy_type": d.DiscrepancyType()}

	tag, err := r.pool.Exec(ctx,
		`INSERT INTO discrepancies (id, transaction_id, provider_reference, discrepancy_type, detected_at, resolved)
		 SELECT $1, $2, $3, $4, $5, false
		 WHERE NOT EXISTS (
		     SELECT 1 FROM discrepancies
		     WHERE transaction_id = $2 AND discrepancy_type = $4 AND resolved = false
		 )`,
		d.ID(), d.TransactionID(), d.ProviderReference(), d.DiscrepancyType(), d.DetectedAt(),
	)
	if err != nil {
		fields["description"] = "insert into discrepancies failed"
		logrus.WithContext(ctx).WithError(err).WithFields(fields).Error(err.Error())
		return err
	}
	if tag.RowsAffected() == 0 {
		fields["description"] = "discrepancy already flagged and unresolved - skipped"
		logrus.WithContext(ctx).WithFields(fields).Info("Save")
	}
	return nil
}
