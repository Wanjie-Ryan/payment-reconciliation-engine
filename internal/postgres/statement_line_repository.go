package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sirupsen/logrus"

	"github.com/Wanjie-Ryan/payment-reconciliation-engine/internal/reconciliation"
)

// StatementLineRepository implements reconciliation.StatementLineRepository.
type StatementLineRepository struct {
	pool *pgxpool.Pool
}

func NewStatementLineRepository(pool *pgxpool.Pool) *StatementLineRepository {
	return &StatementLineRepository{pool: pool}
}

// Save inserts the statement line, or does nothing if this exact
// provider_reference was already recorded - ON CONFLICT DO NOTHING makes a
// duplicate webhook delivery safe by construction, the same way the
// transactions table's unique constraint makes a duplicate idempotency key
// safe (provider_reference is already UNIQUE on this table from Phase 2).
func (r *StatementLineRepository) Save(ctx context.Context, line *reconciliation.StatementLine) error {
	fields := logrus.Fields{"provider_reference": line.ProviderReference()}

	tag, err := r.pool.Exec(ctx,
		`INSERT INTO provider_statement_lines (id, provider_reference, amount, status, reported_at)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (provider_reference) DO NOTHING`,
		line.ID(), line.ProviderReference(), line.Amount(), line.Status(), line.ReportedAt(),
	)
	if err != nil {
		fields["description"] = "insert into provider_statement_lines failed"
		logrus.WithContext(ctx).WithError(err).WithFields(fields).Error(err.Error())
		return err
	}

	if tag.RowsAffected() == 0 {
		fields["description"] = "duplicate provider statement line ignored"
		logrus.WithContext(ctx).WithFields(fields).Warn("duplicate statement line")
	}
	return nil
}

// FindByProviderReference implements reconciliation.StatementLineRepository.
func (r *StatementLineRepository) FindByProviderReference(ctx context.Context, reference string) (*reconciliation.StatementLine, error) {
	var (
		id         uuid.UUID
		amount     int64
		status     string
		reportedAt time.Time
	)

	err := r.pool.QueryRow(ctx,
		`SELECT id, amount, status, reported_at FROM provider_statement_lines WHERE provider_reference = $1`, reference,
	).Scan(&id, &amount, &status, &reportedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w: %s", reconciliation.ErrStatementLineNotFound, reference)
	}
	if err != nil {
		logrus.WithContext(ctx).WithError(err).WithFields(logrus.Fields{
			"description":         "select from provider_statement_lines failed",
			"provider_reference":  reference,
		}).Error(err.Error())
		return nil, err
	}

	return reconciliation.RehydrateStatementLine(id, reference, amount, status, reportedAt), nil
}
