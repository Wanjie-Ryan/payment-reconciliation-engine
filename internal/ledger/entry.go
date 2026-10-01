package ledger

import "github.com/google/uuid"

// entry is immutable and its constructor is unexported.
// entries only ever come from a transaction's own constructor, never created standalone

type Entry struct {
	id            uuid.UUID
	transactionID uuid.UUID
	accountID     uuid.UUID
	amount        Money
}

func newEntry(transactionID, accountID uuid.UUID, amount Money) Entry {
	return Entry{
		id:            uuid.New(),
		transactionID: transactionID,
		accountID:     accountID,
		amount:        amount,
	}
}

// rehydrateEntry rebuilds an Entry from data already stored, skipping the invariant checks - the row was validated when it was first inserted
func RehydrateEntry(id, transactionID, accountID uuid.UUID, amount Money) Entry {
	return Entry{id: id, transactionID: transactionID, accountID: accountID, amount: amount}
}

func (e Entry) ID() uuid.UUID            { return e.id }
func (e Entry) TransactionID() uuid.UUID { return e.transactionID }
func (e Entry) AccountID() uuid.UUID     { return e.accountID }
func (e Entry) Amount() Money            { return e.amount }
