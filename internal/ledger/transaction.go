package ledger

import (
	"errors"
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

// transactions owns its entries and is the only thing that can create them

type Transaction struct {
	id             uuid.UUID
	idempotencyKey string
	txType         string
	status         TransactionStatus
	entries        []Entry
	createdAt      time.Time
}

// NewTransfer builds a balanced two-entry Transaction moving money from one account to another.
// debit (-ve) on source, credit (+ve) on destination, so that they can sum to 0

func NewTransfer(fromAccountID, toAccountID uuid.UUID, amount Money, idempotencyKey string) (*Transaction, error) {
	if idempotencyKey == "" {
		return nil, errors.New("ledger: idempotency key is required")

	}

	if fromAccountID == toAccountID {
		return nil, errors.New("ledger: cannot transfer to the same account")
	}

	if amount.isZero() {
		return nil, errors.New("ledger: transfer amount must be non-zero")
	}

	txnID := uuid.New()

	txn := &Transaction{
		id:             txnID,
		idempotencyKey: idempotencyKey,
		txType:         "transfer",
		status:         StatusPending,
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

func (t *Transaction) ID() uuid.UUID             { return t.id }
func (t *Transaction) IdempotencyKey() string    { return t.idempotencyKey }
func (t *Transaction) Type() string              { return t.txType }
func (t *Transaction) Status() TransactionStatus { return t.status }
func (t *Transaction) CreatedAt() time.Time      { return t.createdAt }

func (t *Transaction) Entries() []Entry {
	return append([]Entry(nil), t.entries...)
}
