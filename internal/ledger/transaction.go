package ledger

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

type TransactionStatus string

const (
	StatusPending TransactionStatus = "pending"
	StatusSettled TransactionStatus = "settled"
	StatusFailed  TransactionStatus = "failed"
)

var ExternalAccountID = uuid.MustParse("00000000-0000-0000-0000-000000000001")

// transactions owns its entries and is the only thing that can create them

type Transaction struct {
	id             uuid.UUID
	idempotencyKey string
	txType         string
	status         TransactionStatus
	entries        []Entry
	createdAt      time.Time
}

// NewTransfer moves money btn 2 internal accounts. Nothing external has to confirm it, so it is settled immediately

func NewTransfer(fromAccountID, toAccountID uuid.UUID, amount Money, idempotencyKey string) (*Transaction, error) {
	if fromAccountID == ExternalAccountID || toAccountID == ExternalAccountID {
		return nil, fmt.Errorf("%w: the external account cannot be used in a transfer", ErrInvalid)
	}

	return newTransaction("transfer", StatusSettled, fromAccountID, toAccountID, amount, idempotencyKey)
}

// NewDeposit brings money in from outside the ledger. it starts pending: a payment provider has to confirm it

func NewDeposit(toAccountID uuid.UUID, amount Money, idempotencyKey string) (*Transaction, error) {
	return newTransaction("deposit", StatusPending, ExternalAccountID, toAccountID, amount, idempotencyKey)
}

// sends money out of the ledger. Statuspending until confirmed
func NewWithdrawal(fromAccountID uuid.UUID, amount Money, idempotencyKey string) (*Transaction, error) {
	return newTransaction("withdrawal", StatusPending, fromAccountID, ExternalAccountID, amount, idempotencyKey)
}

// NewTransfer builds a balanced two-entry Transaction moving money from one account to another.
// debit (-ve) on source, credit (+ve) on destination, so that they can sum to 0

func newTransaction(txType string, status TransactionStatus, fromAccountID, toAccountID uuid.UUID, amount Money, idempotencyKey string) (*Transaction, error) {
	if idempotencyKey == "" {
		return nil, fmt.Errorf("%w: ledger-newTransaction - idempotency key is required", ErrInvalid)

	}

	if fromAccountID == toAccountID {
		return nil, fmt.Errorf("%w: ledger-newTransaction - source and destination account are the same", ErrInvalid)

	}

	if !amount.IsPositive() {
		return nil, fmt.Errorf("%w: ledger-newTransaction - amount must be greater than zero", ErrInvalid)

	}

	txnID := uuid.New()

	txn := &Transaction{
		id:             txnID,
		idempotencyKey: idempotencyKey,
		txType:         txType,
		status:         status,
		entries: []Entry{
			newEntry(txnID, fromAccountID, amount.Negate()),
			newEntry(txnID, toAccountID, amount),
		},
		createdAt: time.Now().UTC(),
	}

	if err := txn.verifyBalanced(); err != nil {
		return nil, err
	}
	return txn, nil

}

// two entries built from the same money can't help but balance, verifyBalanced is the guard every future constructor has to pass through

func (t *Transaction) verifyBalanced() error {
	sums := map[string]int64{}

	for _, e := range t.entries {
		sums[e.Amount().currency] += e.Amount().Amount()
	}

	for currency, sum := range sums {
		if sum != 0 {
			return fmt.Errorf("ledger: entries for %s do not sum to zero (got %d)", currency, sum)
		}
	}
	return nil

}

func RehydrateTransaction(id uuid.UUID, idempotencyKey, txType string, status TransactionStatus, entries []Entry, createdAt time.Time) *Transaction {
	return &Transaction{
		id:             id,
		idempotencyKey: idempotencyKey,
		txType:         txType,
		status:         status,
		entries:        append([]Entry(nil), entries...),
		createdAt:      createdAt,
	}
}

func (t *Transaction) ID() uuid.UUID             { return t.id }
func (t *Transaction) IdempotencyKey() string    { return t.idempotencyKey }
func (t *Transaction) Type() string              { return t.txType }
func (t *Transaction) Status() TransactionStatus { return t.status }
func (t *Transaction) CreatedAt() time.Time      { return t.createdAt }

func (t *Transaction) Entries() []Entry {
	return append([]Entry(nil), t.entries...)
}
