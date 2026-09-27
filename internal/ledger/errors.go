package ledger

import "errors"

var (
	ErrInvalid         = errors.New("invalid input")
	ErrAccountNotFound = errors.New("account not found")
)
