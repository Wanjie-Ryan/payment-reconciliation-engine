package ledger

import "errors"

var (
	ErrInvalid           = errors.New("invalid input")
	ErrAccountNotFound   = errors.New("account not found")
	ErrInsufficientFunds = errors.New("insufficient funds")
	ErrRetry             = errors.New("temporarily unavailable, please retry")
)
