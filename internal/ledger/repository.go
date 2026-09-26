package ledger

import (
	"context"

	"github.com/google/uuid"
)

// transaction and account Repositories are defined by the domain and implemented later by internal/postgres
// this is how the domain stays ignorant of postgres entirely

type TransactionRepository interface {
	Save(ctx context.Context, txn *Transaction) error
	FindByIdempotencyKey(ctx context.Context, key string)(*Transaction, error)
}

type AccountRepository interface{
	Save(ctx context.Context, account *Account) error
	FindByID(ctx context.Context, id uuid.UUID) (*Account, error)
}