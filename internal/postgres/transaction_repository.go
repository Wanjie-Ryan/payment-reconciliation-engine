package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

// pgLockNotavailable is postgre's SQLSTATE for a statement that hit lock_timeout before it could acquire a row lock
const pgLockNotAvailable = "55P03"
const pgUniqueViolation = "23505"

// save writes the transaction row and all of its entry rows inside on DB transactions; either every row lands or none do.

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
