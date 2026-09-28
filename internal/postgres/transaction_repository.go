package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sirupsen/logrus"

	"github.com/Wanjie-Ryan/payment-reconciliation-engine/internal/ledger"
)

type TransactionRepository struct {
	pool *pgxpool.Pool
}

func NewTransactionRepository(pool *pgxpool.Pool) *TransactionRepository {
	return &TransactionRepository{pool: pool}
}

func (r *TransactionRepository) Save(ctx context.Context, txn *ledger.Transaction) error {
	fields := logrus.Fields{"transaction_id": txn.ID()}

	tx, err := r.pool.Begin(ctx)

	if err != nil {
		fields["description"] = "begin DB transaction failed"
		logrus.WithContext(ctx).WithError(err).WithFields(fields).Error(err.Error())
		return err
	}

	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `INSERT INTO transactions (id, idempotency_key, type, status, created_at) VALUES ($1, $2, $3, $4, $5)`, txn.ID(), txn.IdempotencyKey(), txn.Type(), string(txn.Status()), txn.CreatedAt())

	if err != nil {

		fields["description"] = "insert into transactions failed"
		logrus.WithContext(ctx).WithError(err).WithFields(fields).Error(err.Error())

		return err
	}

	for _, e := range txn.Entries() {
		_, err = tx.Exec(ctx, `INSERT INTO entries (id, transaction_id, account_id, amount) VALUES ($1, $2, $3, $4)`, e.ID(), e.TransactionID(), e.AccountID(), e.Amount().Amount())

		if err != nil {
			fields["description"] = "insert into entries failed"
			fields["entry_id"] = e.ID()
			logrus.WithContext(ctx).WithError(err).WithFields(fields).Error(err.Error())
			return err
		}

	}

	if err := tx.Commit(ctx); err != nil {
		fields["description"] = "commit failed"
		logrus.WithContext(ctx).WithError(err).WithFields(fields).Error(err.Error())
	}

	return nil

}
