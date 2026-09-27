package ledger

import (
	"context"

	"github.com/google/uuid"
)

// transaction and account Repositories are defined by the domain and implemented later by internal/postgres
// this is how the domain stays ignorant of postgres entirely

type TransactionRepository interface {
	// saves and writes the transaction and all of its entries atomically
	Save(ctx context.Context, txn *Transaction) error
	FindByIdempotencyKey(ctx context.Context, key string) (*Transaction, error)
}

type AccountRepository interface {
	Save(ctx context.Context, account *Account) error
	// findById returns the account when found by the id
	FindByID(ctx context.Context, id uuid.UUID) (*Account, error)

	GetBalance(ctx context.Context, id uuid.UUID) (int64, error)
}
