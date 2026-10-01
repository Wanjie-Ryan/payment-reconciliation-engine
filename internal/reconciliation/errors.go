package reconciliation

import "errors"

// This bounded context defines its own errors rather than reusing ledger's -
// the two contexts stay decoupled, same as they don't share repository
// interfaces or domain types.
var (
	ErrInvalid               = errors.New("invalid input")
	ErrStatementLineNotFound = errors.New("statement line not found")
)
