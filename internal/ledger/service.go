package ledger

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

type LedgerService struct {
	accounts     AccountRepository
	transactions TransactionRepository
}

func NewLedgerService(accounts AccountRepository, transactions TransactionRepository) *LedgerService {
	return &LedgerService{accounts: accounts, transactions: transactions}
}

// logFailure logs expected failures (bad input, unknown account) as warnings and everything else as errors
func logFailure(ctx context.Context, description string, err error, fields logrus.Fields) {

	fields["description"] = description
	entry := logrus.WithContext(ctx).WithError(err).WithFields(fields)
	if errors.Is(err, ErrInvalid) || errors.Is(err, ErrAccountNotFound) {
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

func (s *LedgerService) Transfer (ctx context.Context, fromID, toID uuid.UUID, amount Money, idempotencyKey string) (*Transaction, error){
	txn, err := NewTransfer(fromID, toID, amount, idempotencyKey)

	if err !=nil{
		logFailure(ctx, "invalid Transfer", err, logrus.Fields{
			"from_account_id": fromID, "to_account_ID": toID, "idempotency_key":idempotencyKey,
		})
		return nil, err
	}

	return s.post(ctx, txn)

}

func (s *LedgerService) Deposit(ctx context.Context, toID uuid.UUID, amount Money, idempotencyKey string) (*Transaction, error){
	txn, err := NewDeposit(toID, amount, idempotencyKey)
	if err !=nil{
		logFailure(ctx, "invalid deposit", err, logrus.Fields{
			"account_id": toID, "idempotency_key": idempotencyKey,
		})
		return nil, err
	}
	return s.post(ctx, txn)
}


func (s *LedgerService) Withdraw (ctx context.Context, fromID uuid.UUID, amount Money, idempotencyKey string) (*Transaction, error){
	txn, err := NewWithdrawal(fromID, amount, idempotencyKey)
	
}