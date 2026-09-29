package ledger

import (
	// "errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// account is an entity - it has an ID that persists independently
type Account struct {
	id       uuid.UUID
	owner    string
	currency string
}

func NewAccount(owner, currency string) (*Account, error) {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if owner == "" {
		return nil, fmt.Errorf("%w: ledger - owner is required", ErrInvalid)
	}

	if currency == "" {
		return nil, fmt.Errorf("%w: ledger - currency is required", ErrInvalid)
	}
	return &Account{id: uuid.New(), owner: owner, currency: currency}, nil

}

func RehydrateAccount(id uuid.UUID, owner, currency string) *Account {
	return &Account{id: id, owner: owner, currency: currency}
}

func (a *Account) ID() uuid.UUID    { return a.id }
func (a *Account) Owner() string    { return a.owner }
func (a *Account) Currency() string { return a.currency }
