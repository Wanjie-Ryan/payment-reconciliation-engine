package ledger

// Domain is the real-world subject area your sw exists to deal with, independent of any code, DB, framework.
// its not a package or folder, those are just WHERE we choose to represent the domain in code.
// The domain itself is the ACTUAL BS problem.
// Examples across different industries
// 1. Banking - domain is money, ownership, balance, transfers. This exists as real world concept independent of any app, banks kept ledgers on paper before computers.
// 2. Healthcare scheduling - domain is patients, doctors, appointments, availability.
// 3. E-commerce - Domain is products, inventory, orders, payments, shipping.

// Each of those is a whole world of concepts and rules that a domain expert (accountant, hospital admin) would understand and reason about even without ever touching a keyboard.
// DDD's whole premise is: your code should model that world using the same vocab that world already uses, rather than flattening everything down into generic "controllers" and "models" that don't reflect any of it.



import "errors"

// money is a value object and has no identity (ID) - two money values with the same fields are simply equal, hence its just a plain comparable struct

type Money struct {
	amount   int64
	currency string
}

// constructor

func NewMoney(amount int64, currency string) (Money, error) {
	if currency == "" {
		return Money{}, errors.New("ledger: currency is required")
	}

	return Money{amount: amount, currency: currency}, nil
}

func (m Money) Amount() int64    { return m.amount }
func (m Money) Currency() string { return m.currency }
func (m Money) isZero() bool     { return m.amount == 0 }

func (m Money) Negate() Money {
	return Money{amount: -m.amount, currency: m.currency}
}
