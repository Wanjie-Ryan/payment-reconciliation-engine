package ledger

import "context"

// PaymentProviderClient is how the ledger asks an external payment provider
// to initiate a charge. Defined here by the domain/use-case layer, same as
// the repositories, and implemented by internal/providerclient - this is
// the only way LedgerService reaches outside the process, and the ledger
// package never imports net/http to do it.
type PaymentProviderClient interface {
	// InitiateCharge asks the provider to charge toward reference. The
	// provider's result (success, failure, or nothing at all) arrives
	// later, asynchronously, as a webhook - this call only confirms the
	// request was accepted for processing.
	InitiateCharge(ctx context.Context, reference string, amount Money, behavior string) error
}
