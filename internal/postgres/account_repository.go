package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sirupsen/logrus"

	"github.com/Wanjie-Ryan/payment-reconciliation-engine/internal/ledger"
)

type AccountRepository struct {
	pool *pgxpool.Pool
}

func NewAccountRepository(pool *pgxpool.Pool) *AccountRepository {
	return &AccountRepository{pool: pool}
}

func (r *AccountRepository) Save(ctx context.Context, account *ledger.Account) error {

	_, err := r.pool.Exec(ctx, `INSERT INTO accounts (id, owner, currency) VALUES ($1, $2, $3)`, account.ID(), account.Owner(), account.Currency())

	if err != nil {
		logrus.WithContext(ctx).WithError(err).WithFields(logrus.Fields{"description": "insert into accounts failed", "account_id": account.ID()}).Error(err.Error())
		return err
	}

	return nil

}

func (r *AccountRepository) FindByID(ctx context.Context, id uuid.UUID) (*ledger.Account, error) {
	var owner, currency string

	err := r.pool.QueryRow(ctx, `SELECT owner, currency FROM accounts WHERE id = $1`, id).Scan(&owner, &currency)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w:%s", ledger.ErrAccountNotFound, id)

	}

	if err != nil {
		logrus.WithContext(ctx).WithError(err).WithFields(logrus.Fields{
			"description": "select from accounts failed",
			"account_id":  id,
		}).Error(err.Error())
		return nil, err
	}

	return ledger.RehydrateAccount(id, owner, currency), nil
}

// getBalance sums every entry on the account, ignoring failed transactions
func (r *AccountRepository) GetBalance(ctx context.Context, id uuid.UUID) (int64, error) {
	var balance int64

	// COALESCE retuens the first non-NULL argument. Sum over zero returns NULL, not 0.
	err := r.pool.QueryRow(ctx, `SELECT COALESCE(SUM(e.amount), 0)::BIGINT FROM entries e JOIN transactions t ON t.id = e.transaction_id WHERE e.account_id = $1 AND t.status <> 'failed'`, id).Scan(&balance)

	if err != nil {
		logrus.WithContext(ctx).WithError(err).WithFields(logrus.Fields{
			"description": "balance query failed",
			"account_id":  id,
		}).Error(err.Error())
		return 0, err
	}
	return balance, nil

}
