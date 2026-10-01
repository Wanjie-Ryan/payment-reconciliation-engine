package ledger

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

type LedgerService struct {
	accounts     AccountRepository
	transactions TransactionRepository
	provider     PaymentProviderClient
}

func NewLedgerService(accounts AccountRepository, transactions TransactionRepository, provider PaymentProviderClient) *LedgerService {
	return &LedgerService{accounts: accounts, transactions: transactions, provider: provider}
}

// logFailure logs expected failures (bad input, unknown account) as warnings and everything else as errors
func logFailure(ctx context.Context, description string, err error, fields logrus.Fields) {

	fields["description"] = description
	entry := logrus.WithContext(ctx).WithError(err).WithFields(fields)
	if errors.Is(err, ErrInvalid) || errors.Is(err, ErrAccountNotFound) || errors.Is(err, ErrInsufficientFunds) || errors.Is(err, ErrRetry) {
		entry.Warn(err.Error())
		return
	}

	entry.Error(err.Error())

}

func (s *LedgerService) CreateAccount(ctx context.Context, owner, currency string) (*Account, error) {
	fields := logrus.Fields{"owner": owner, "currency": currency}

	account, err := NewAccount(owner, currency)

	if err != nil {
		logFailure(ctx, "invalid account or currency", err, fields)
		return nil, err
	}

	fields["account_id"] = account.ID()

	if err := s.accounts.Save(ctx, account); err != nil {
		logFailure(ctx, "failed to save account", err, fields)
		return nil, err
	}

	fields["description"] = "account created"
	logrus.WithContext(ctx).WithFields(fields).Info("Account created")
	return account, nil

}

// getAccount returns an account together with its current balance
func (s *LedgerService) GetAccount(ctx context.Context, id uuid.UUID) (*Account, Money, error) {
	fields := logrus.Fields{"account_id": id}

	// get the account by id
	account, err := s.accounts.FindByID(ctx, id)

	if err != nil {
		logFailure(ctx, "failed to load account", err, fields)
		return nil, Money{}, err
	}

	abstractBalance, err := s.accounts.GetBalance(ctx, id)

	if err != nil {
		logFailure(ctx, "failed to compute balance", err, fields)
		return nil, Money{}, err
	}

	balance, err := NewMoney(abstractBalance, account.Currency())

	if err != nil {
		logFailure(ctx, "failed to build balance", err, fields)
		return nil, Money{}, err
	}

	return account, balance, nil

}

func (s *LedgerService) Transfer(ctx context.Context, fromID, toID uuid.UUID, amount Money, idempotencyKey string) (*Transaction, bool, error) {
	txn, err := NewTransfer(fromID, toID, amount, idempotencyKey)

	if err != nil {
		logFailure(ctx, "invalid Transfer", err, logrus.Fields{
			"from_account_id": fromID, "to_account_ID": toID, "idempotency_key": idempotencyKey,
		})
		return nil, false, err
	}

	return s.post(ctx, txn)

}

func (s *LedgerService) Deposit(ctx context.Context, toID uuid.UUID, amount Money, idempotencyKey, behavior string) (*Transaction, bool, error) {
	txn, err := NewDeposit(toID, amount, idempotencyKey)
	if err != nil {
		logFailure(ctx, "invalid deposit", err, logrus.Fields{
			"account_id": toID, "idempotency_key": idempotencyKey,
		})
		return nil, false, err
	}
	// return s.post(ctx, txn)

	result, replayed, err := s.post(ctx, txn)

	if err != nil || replayed {
		return result, replayed, err
	}

	if err := s.provider.InitiateCharge(ctx, result.IdempotencyKey(), amount, behavior); err != nil {
		logFailure(ctx, "failed to intiate provider charge", err, logrus.Fields{
			"transaction_id":  result.ID(),
			"idempotency_key": result.IdempotencyKey(),
		})
	}

	return result, replayed, nil

}

func (s *LedgerService) Withdraw(ctx context.Context, fromID uuid.UUID, amount Money, idempotencyKey string) (*Transaction, bool, error) {
	txn, err := NewWithdrawal(fromID, amount, idempotencyKey)
	if err != nil {
		logFailure(ctx, "invalid withdrawal", err, logrus.Fields{
			"account_id": fromID, "idempotency_key": idempotencyKey,
		})
		return nil, false, err
	}
	return s.post(ctx, txn)
}

func (s *LedgerService) post(ctx context.Context, txn *Transaction) (*Transaction, bool, error) {
	fields := logrus.Fields{
		"transaction_id":  txn.ID(),
		"type":            txn.Type(),
		"idempotency_key": txn.IdempotencyKey(),
	}

	for _, e := range txn.Entries() {
		account, err := s.accounts.FindByID(ctx, e.AccountID())
		if err != nil {
			logFailure(ctx, "failed to load account", err, fields)
			return nil, false, err
		}

		if account.Currency() != e.Amount().Currency() {

			err := fmt.Errorf("%w: account %s holds %s but the transaction is in %s", ErrInvalid, account.ID(), account.Currency(), e.Amount().Currency())

			logFailure(ctx, "currenct mismatch", err, fields)
			return nil, false, err

		}

	}

	if err := s.transactions.Save(ctx, txn); err != nil {

		if errors.Is(err, ErrDuplicateIdempotencyKey) {
			existing, ferr := s.transactions.FindByIdempotencyKey(ctx, txn.IdempotencyKey())

			if ferr != nil {
				logFailure(ctx, "failed to load existing transaction for idempotent replay", ferr, fields)
				return nil, false, ferr
			}

			fields["description"] = "idempotent replay"
			fields["existing_transaction_id"] = existing.ID()
			logrus.WithContext(ctx).WithFields(fields).Info("idempotent replay")
			return existing, true, nil

		}

		logFailure(ctx, "failed to save transaction", err, fields)
		return nil, false, err

	}

	fields["description"] = "transaction posted"
	fields["status"] = txn.Status()
	logrus.WithContext(ctx).WithFields(fields).Info("transaction posted")
	return txn, false, nil

}
