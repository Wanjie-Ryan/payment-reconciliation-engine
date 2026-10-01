package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sirupsen/logrus"

	"github.com/Wanjie-Ryan/payment-reconciliation-engine/internal/ledger"
	"github.com/Wanjie-Ryan/payment-reconciliation-engine/internal/reconciliation"
)

type TransactionRepository struct {
	pool *pgxpool.Pool
}

func NewTransactionRepository(pool *pgxpool.Pool) *TransactionRepository {
	return &TransactionRepository{pool: pool}
}

// pgLockNotavailable is postgre's SQLSTATE for a statement that hit lock_timeout before it could acquire a row lock
const pgLockNotAvailable = "55P03"
const pgUniqueViolation = "23505"

// save writes the transaction row, all of its entry rows, and one
// transaction.posted event row, inside one DB transaction; either every row
// lands or none do. The event row is what Phase 7's replay rebuilds
// balances from - it has to be written atomically with entries, in the same
// DB transaction, or the event log could drift from the entries it's
// supposed to be an independent record of.

// before writing anything, if the transaction debits an internal account (a withdrawal), it locks the account's row and re-dervices its balance, rejecting the save if the debit would take it below 0
const idempotencyKeyConstraint = "transactions_idempotency_key_key"

func (r *TransactionRepository) Save(ctx context.Context, txn *ledger.Transaction) error {
	fields := logrus.Fields{"transaction_id": txn.ID()}

	tx, err := r.pool.Begin(ctx)

	if err != nil {
		fields["description"] = "begin DB transaction failed"
		logrus.WithContext(ctx).WithError(err).WithFields(fields).Error(err.Error())
		return err
	}

	// on any early return this undoes everything, including releasing the row lock below.

	defer tx.Rollback(ctx)

	// SET LOCAL  is scoped to this transaction and resets at commit/rollback - a request can't get stuck forever behind whoever holds the lock.

	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '2s'`); err != nil {
		fields["description"] = "set lock_timeout failed"
		logrus.WithContext(ctx).WithError(err).WithFields(fields).Error(err.Error())
		return err
	}

	_, err = tx.Exec(ctx, `INSERT INTO transactions (id, idempotency_key, type, status, created_at) VALUES ($1, $2, $3, $4, $5)`, txn.ID(), txn.IdempotencyKey(), txn.Type(), string(txn.Status()), txn.CreatedAt())

	if err != nil {

		if isDuplicateIdempotencyKey(err) {
			return fmt.Errorf("%w: %s", ledger.ErrDuplicateIdempotencyKey, txn.IdempotencyKey())
		}
		fields["description"] = "insert into transactions failed"
		logrus.WithContext(ctx).WithError(err).WithFields(fields).Error(err.Error())

		return err
	}

	if accountID, debit, ok := debitedInternalAccount(txn); ok {
		if err := lockAndCheckBalance(ctx, tx, accountID, debit, fields); err != nil {
			return err
		}
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

	event := ledger.NewTransactionPostedEvent(txn)
	payloadJSON, err := json.Marshal(event.Payload)
	if err != nil {
		fields["description"] = "failed to marshal event payload"
		logrus.WithContext(ctx).WithError(err).WithFields(fields).Error(err.Error())
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO events (id, transaction_id, event_type, payload, created_at) VALUES ($1, $2, $3, $4, $5)`,
		event.ID, event.TransactionID, event.EventType, payloadJSON, event.CreatedAt)
	if err != nil {
		fields["description"] = "insert into events failed"
		logrus.WithContext(ctx).WithError(err).WithFields(fields).Error(err.Error())
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		fields["description"] = "commit failed"
		logrus.WithContext(ctx).WithError(err).WithFields(fields).Error(err.Error())
		return err
	}

	return nil

}

// findByIdempotency loads a previosuly saved transaction and its entries by idempotency key - used for idempotent replay after save reports

func (r *TransactionRepository) FindByIdempotencyKey(ctx context.Context, key string) (*ledger.Transaction, error) {
	var (
		id        uuid.UUID
		txType    string
		status    string
		createdAt time.Time
	)

	err := r.pool.QueryRow(ctx,
		`SELECT id, type, status, created_at FROM transactions WHERE idempotency_key = $1`, key,
	).Scan(&id, &txType, &status, &createdAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w: idempotency key %s", ledger.ErrTransactionNotFound, key)
	}
	if err != nil {
		logrus.WithContext(ctx).WithError(err).WithFields(logrus.Fields{
			"description": "select from transactions by idempotency key failed", "idempotency_key": key,
		}).Error(err.Error())
		return nil, err
	}

	// entries has no currency column - an entry's currency is always its
	// account's currency (post() enforces that at write time), so join to get it back.
	rows, err := r.pool.Query(ctx,
		`SELECT e.id, e.account_id, e.amount, a.currency
		   FROM entries e JOIN accounts a ON a.id = e.account_id
		  WHERE e.transaction_id = $1`, id,
	)
	if err != nil {
		logrus.WithContext(ctx).WithError(err).WithFields(logrus.Fields{
			"description": "select entries for transaction failed", "transaction_id": id,
		}).Error(err.Error())
		return nil, err
	}
	defer rows.Close()

	var entries []ledger.Entry
	for rows.Next() {
		var entryID, accountID uuid.UUID
		var amount int64
		var currency string
		if err := rows.Scan(&entryID, &accountID, &amount, &currency); err != nil {
			logrus.WithContext(ctx).WithError(err).WithFields(logrus.Fields{
				"description": "scan entry row failed", "transaction_id": id,
			}).Error(err.Error())
			return nil, err
		}
		money, err := ledger.NewMoney(amount, currency)
		if err != nil {
			return nil, err
		}
		entries = append(entries, ledger.RehydrateEntry(entryID, id, accountID, money))
	}
	if err := rows.Err(); err != nil {
		logrus.WithContext(ctx).WithError(err).WithFields(logrus.Fields{
			"description": "iterate entries failed", "transaction_id": id,
		}).Error(err.Error())
		return nil, err
	}

	return ledger.RehydrateTransaction(id, key, txType, ledger.TransactionStatus(status), entries, createdAt), nil
}

// FindPendingDeposits returns every deposit still in "pending" status that
// was created at or before cutoff - the set reconciliation.Service.Reconcile
// checks each run. The amount is read from the deposit's credit entry (the
// positive one, into the destination account) since transactions itself
// doesn't store an amount.
func (r *TransactionRepository) FindPendingDeposits(ctx context.Context, cutoff time.Time) ([]reconciliation.PendingTransaction, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT t.id, t.idempotency_key, e.amount, t.created_at
		   FROM transactions t
		   JOIN entries e ON e.transaction_id = t.id AND e.amount > 0
		  WHERE t.type = 'deposit' AND t.status = 'pending' AND t.created_at <= $1`, cutoff,
	)
	if err != nil {
		logrus.WithContext(ctx).WithError(err).WithFields(logrus.Fields{
			"description": "select pending deposits failed",
		}).Error(err.Error())
		return nil, err
	}
	defer rows.Close()

	var pending []reconciliation.PendingTransaction
	for rows.Next() {
		var p reconciliation.PendingTransaction
		if err := rows.Scan(&p.ID, &p.IdempotencyKey, &p.Amount, &p.CreatedAt); err != nil {
			logrus.WithContext(ctx).WithError(err).WithFields(logrus.Fields{
				"description": "scan pending deposit row failed",
			}).Error(err.Error())
			return nil, err
		}
		pending = append(pending, p)
	}
	if err := rows.Err(); err != nil {
		logrus.WithContext(ctx).WithError(err).WithFields(logrus.Fields{
			"description": "iterate pending deposits failed",
		}).Error(err.Error())
		return nil, err
	}
	return pending, nil
}

// MarkSettled and MarkFailed implement reconciliation.TransactionWriter.
func (r *TransactionRepository) MarkSettled(ctx context.Context, transactionID uuid.UUID) error {
	return r.updateStatus(ctx, transactionID, ledger.StatusSettled, "transaction.settled")
}

func (r *TransactionRepository) MarkFailed(ctx context.Context, transactionID uuid.UUID) error {
	return r.updateStatus(ctx, transactionID, ledger.StatusFailed, "transaction.failed")
}

// updateStatus changes the transaction's status AND appends a matching
// event, atomically in one DB transaction - the same reasoning as Save()
// writing entries and their event together. Without the event, replay
// would never learn about this status change and would drift from live
// state for any transaction reconciliation later settles or fails.
func (r *TransactionRepository) updateStatus(ctx context.Context, transactionID uuid.UUID, status ledger.TransactionStatus, eventType string) error {
	fields := logrus.Fields{"transaction_id": transactionID, "status": status}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		fields["description"] = "begin DB transaction failed"
		logrus.WithContext(ctx).WithError(err).WithFields(fields).Error(err.Error())
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `UPDATE transactions SET status = $1 WHERE id = $2`, string(status), transactionID); err != nil {
		fields["description"] = "update transaction status failed"
		logrus.WithContext(ctx).WithError(err).WithFields(fields).Error(err.Error())
		return err
	}

	event := ledger.NewTransactionStatusEvent(transactionID, eventType, status)
	payloadJSON, err := json.Marshal(event.Payload)
	if err != nil {
		fields["description"] = "failed to marshal status event payload"
		logrus.WithContext(ctx).WithError(err).WithFields(fields).Error(err.Error())
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO events (id, transaction_id, event_type, payload, created_at) VALUES ($1, $2, $3, $4, $5)`,
		event.ID, event.TransactionID, event.EventType, payloadJSON, event.CreatedAt); err != nil {
		fields["description"] = "insert status event failed"
		logrus.WithContext(ctx).WithError(err).WithFields(fields).Error(err.Error())
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		fields["description"] = "commit failed"
		logrus.WithContext(ctx).WithError(err).WithFields(fields).Error(err.Error())
		return err
	}
	return nil
}

// debitedInternalAccount returns the one internal (non-external) account a transaction debits, if any.
// Every transaction has exactly one -ve entry; if its account is EXTERNAL (deposit), there's nothing to lock or check - External is allowed to go -ve, thats how money enters the ledger at all.

func debitedInternalAccount(txn *ledger.Transaction) (accountID uuid.UUID, amount int64, ok bool) {
	for _, e := range txn.Entries() {
		if e.Amount().Amount() < 0 && e.AccountID() != ledger.ExternalAccountID {
			return e.AccountID(), e.Amount().Amount(), true
		}
	}

	return uuid.Nil, 0, false
}

// lockAndCheckBalance locks the account's row, computes its balance with the same SUM query GetBalance uses, it rejects the debit if it would take the balance below 0

func lockAndCheckBalance(ctx context.Context, tx pgx.Tx, accountID uuid.UUID, debitAmount int64, fields logrus.Fields) error {
	var exists bool

	err := tx.QueryRow(ctx, `SELECT true FROM accounts where id = $1 FOR UPDATE`, accountID).Scan(&exists)

	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: %s", ledger.ErrAccountNotFound, accountID)
	}

	if err != nil {
		if isLockTimeout(err) {
			logrus.WithContext(ctx).WithError(err).WithFields(fields).Warn("timed out waiting for account lock")
			return fmt.Errorf("%w", ledger.ErrRetry)
		}
		fields["description"] = "lock account row failed"
		logrus.WithContext(ctx).WithError(err).WithFields(fields).Error(err.Error())
		return err
	}

	var balance int64

	err = tx.QueryRow(ctx, `SELECT COALESCE(SUM(e.amount), 0)::BIGINT FROM entries e JOIN transactions t ON t.id = e.transaction_id WHERE e.account_id = $1 AND t.status <> 'failed'`, accountID).Scan(&balance)

	if err != nil {
		fields["description"] = "balance query failed"
		logrus.WithContext(ctx).WithError(err).WithFields(fields).Error(err.Error())
		return err
	}

	if balance+debitAmount < 0 {
		fields["account_id"] = accountID
		fields["balance"] = balance
		fields["requested_amount"] = -debitAmount

		logrus.WithContext(ctx).WithFields(fields).Warn("insufficient funds")

		return fmt.Errorf("%w: account %s has balance %d, requested debit of %d", ledger.ErrInsufficientFunds, accountID, balance, -debitAmount)
	}

	return nil

}

// isLockTimeout reports whether err is postgres lock_timeout error
func isLockTimeout(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgLockNotAvailable
}
func isDuplicateIdempotencyKey(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation && pgErr.ConstraintName == idempotencyKeyConstraint
}
