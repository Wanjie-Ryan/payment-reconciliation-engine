package ledger

import (
	"errors"

	"github.com/google/uuid"
)

// account is an entity - it has an ID that persists independently
type Account struct {
	id       uuid.UUID
	owner    string
	currency string
}

func NewAccount(owner, currency string) (*Account, error) {
	if owner == "" {
		return nil, errors.New("ledger: owner is required")
	}

	if currency == "" {
		return nil, errors.New("ledger: currency is required")
	}
	return &Account{id: uuid.New(), owner: owner, currency: currency}, nil

}

func (a *Account) ID() uuid.UUID    { return a.id }
func (a *Account) Owner() string    { return a.owner }
func (a *Account) Currency() string { return a.currency }
