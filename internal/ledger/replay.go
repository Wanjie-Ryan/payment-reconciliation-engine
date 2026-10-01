package ledger

import (
	"context"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// EventRepository is read-only here on purpose: the only place an Event is
// ever written is inside postgres.TransactionRepository.Save, in the same
// DB transaction as the entries it describes - writing it anywhere else
// risks the event log drifting from what actually happened.
type EventRepository interface {
	// FindAllOrdered returns every event ever recorded, oldest first - the
	// full history replay walks through.
	FindAllOrdered(ctx context.Context) ([]Event, error)
}

// AccountBalanceReport compares one account's balance as rebuilt purely
// from the event log against its live balance (summed from entries).
type AccountBalanceReport struct {
	AccountID     uuid.UUID `json:"account_id"`
	ReplayBalance int64     `json:"replay_balance"`
	LiveBalance   int64     `json:"live_balance"`
	Match         bool      `json:"match"`
}

// ReplayReport is the result of one full event-log replay.
type ReplayReport struct {
	EventCount int                     `json:"event_count"`
	AllMatch   bool                    `json:"all_match"`
	Accounts   []AccountBalanceReport  `json:"accounts"`
}

// ReplayService proves the event log is sufficient on its own to
// reconstruct current state: it rebuilds every account's balance purely by
// replaying events, then compares that against the live balance (which
// comes from summing entries - a different table, written in the same DB
// transaction as the events, but never read by replay).
type ReplayService struct {
	events   EventRepository
	accounts AccountRepository
}

func NewReplayService(events EventRepository, accounts AccountRepository) *ReplayService {
	return &ReplayService{events: events, accounts: accounts}
}

// transactionState is what replay tracks per transaction while walking the
// event log in order: its entries (from the original transaction.posted
// event) and its latest known status (updated by any later
// transaction.settled/transaction.failed event).
type transactionState struct {
	entries []EntrySnapshot
	status  string
}

func (s *ReplayService) Replay(ctx context.Context) (*ReplayReport, error) {
	events, err := s.events.FindAllOrdered(ctx)
	if err != nil {
		logrus.WithContext(ctx).WithError(err).WithFields(logrus.Fields{
			"description": "failed to load events for replay",
		}).Error(err.Error())
		return nil, err
	}

	// Pass 1: replay events in order to reconstruct each transaction's
	// entries and current status purely from the log - this is what makes
	// it a real replay rather than a live-table lookup in disguise.
	states := map[uuid.UUID]*transactionState{}
	for _, event := range events {
		switch event.EventType {
		case "transaction.posted":
			states[event.TransactionID] = &transactionState{
				entries: event.Payload.Entries,
				status:  event.Payload.Status,
			}
		case "transaction.settled", "transaction.failed":
			if st, ok := states[event.TransactionID]; ok {
				st.status = event.Payload.Status
			}
		}
	}

	// Pass 2: sum entries into balances, applying the same rule
	// AccountRepository.GetBalance does in Postgres - a failed transaction's
	// entries don't count. Any other status (pending, settled) does, same
	// as the live query.
	balances := map[uuid.UUID]int64{}
	for _, st := range states {
		if st.status == string(StatusFailed) {
			continue
		}
		for _, entry := range st.entries {
			balances[entry.AccountID] += entry.Amount
		}
	}

	report := &ReplayReport{EventCount: len(events), AllMatch: true}
	for accountID, replayBalance := range balances {
		liveBalance, err := s.accounts.GetBalance(ctx, accountID)
		if err != nil {
			logrus.WithContext(ctx).WithError(err).WithFields(logrus.Fields{
				"description": "failed to load live balance during replay",
				"account_id":  accountID,
			}).Error(err.Error())
			return nil, err
		}

		match := liveBalance == replayBalance
		if !match {
			report.AllMatch = false
		}
		report.Accounts = append(report.Accounts, AccountBalanceReport{
			AccountID: accountID, ReplayBalance: replayBalance, LiveBalance: liveBalance, Match: match,
		})
	}

	logrus.WithContext(ctx).WithFields(logrus.Fields{
		"description": "replay complete",
		"event_count": report.EventCount,
		"accounts":    len(report.Accounts),
		"all_match":   report.AllMatch,
	}).Info("Replay")

	return report, nil
}
