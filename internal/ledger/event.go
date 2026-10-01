package ledger

import (
	"time"

	"github.com/google/uuid"
)

// EntrySnapshot is a point-in-time copy of one entry, carried inside an
// event's payload. Event payloads are stored as JSONB, so these fields are
// exported with JSON tags - unlike Account/Entry/Transaction, nothing ever
// mutates an Event after it's created, so there's no invariant here that
// needs protecting with unexported fields and getters.
type EntrySnapshot struct {
	AccountID uuid.UUID `json:"account_id"`
	Amount    int64     `json:"amount"`
	Currency  string    `json:"currency"`
}

// TransactionPostedPayload is the only event payload shape this project
// produces right now - one event per transaction, carrying everything
// needed to replay it without touching the entries table at all.
type TransactionPostedPayload struct {
	IdempotencyKey string          `json:"idempotency_key"`
	Type           string          `json:"type"`
	Status         string          `json:"status"`
	Entries        []EntrySnapshot `json:"entries"`
}

// Event is an append-only record in the ledger's event log - the real
// source of truth event sourcing refers to. Nothing ever updates or deletes
// an Event once written.
type Event struct {
	ID            uuid.UUID
	TransactionID uuid.UUID
	EventType     string
	Payload       TransactionPostedPayload
	CreatedAt     time.Time
}

// NewTransactionPostedEvent captures everything about txn needed to replay
// its effect on account balances later, purely from this event - no
// dependency on the entries table surviving or staying consistent with it.
func NewTransactionPostedEvent(txn *Transaction) Event {
	entries := make([]EntrySnapshot, 0, len(txn.Entries()))
	for _, e := range txn.Entries() {
		entries = append(entries, EntrySnapshot{
			AccountID: e.AccountID(),
			Amount:    e.Amount().Amount(),
			Currency:  e.Amount().Currency(),
		})
	}
	return Event{
		ID:            uuid.New(),
		TransactionID: txn.ID(),
		EventType:     "transaction.posted",
		Payload: TransactionPostedPayload{
			IdempotencyKey: txn.IdempotencyKey(),
			Type:           txn.Type(),
			Status:         string(txn.Status()),
			Entries:        entries,
		},
		CreatedAt: txn.CreatedAt(),
	}
}

// NewTransactionStatusEvent records a later status change (reconciliation
// settling or failing a transaction) as its own event. Without this,
// replay would only ever see a transaction's status as it was at creation
// time - "pending" forever - and would drift from live state the moment
// anything transitioned it afterward. eventType is "transaction.settled" or
// "transaction.failed"; the payload carries just the new status, since
// Entries were already captured by the original transaction.posted event.
func NewTransactionStatusEvent(transactionID uuid.UUID, eventType string, status TransactionStatus) Event {
	return Event{
		ID:            uuid.New(),
		TransactionID: transactionID,
		EventType:     eventType,
		Payload:       TransactionPostedPayload{Status: string(status)},
		CreatedAt:     time.Now().UTC(),
	}
}
