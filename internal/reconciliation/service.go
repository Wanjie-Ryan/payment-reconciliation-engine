package reconciliation

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// Service is this bounded context's application service - same shape as
// ledger.LedgerService: coordinates domain objects and repositories, holds
// no rules of its own beyond what Reconcile expresses.
type Service struct {
	lines         StatementLineRepository
	discrepancies DiscrepancyRepository
	transactions  TransactionReader
	settler       TransactionWriter
	gracePeriod   time.Duration
}

func NewService(lines StatementLineRepository, discrepancies DiscrepancyRepository, transactions TransactionReader, settler TransactionWriter, gracePeriod time.Duration) *Service {
	return &Service{
		lines: lines, discrepancies: discrepancies, transactions: transactions, settler: settler, gracePeriod: gracePeriod,
	}
}

// RecordStatementLine stores what the provider reported for one reference.
func (s *Service) RecordStatementLine(ctx context.Context, providerReference string, amount int64, status string, reportedAt time.Time) error {
	fields := logrus.Fields{"provider_reference": providerReference, "amount": amount, "status": status}

	line, err := NewStatementLine(providerReference, amount, status, reportedAt)
	if err != nil {
		fields["description"] = "invalid statement line"
		logrus.WithContext(ctx).WithError(err).WithFields(fields).Warn(err.Error())
		return err
	}

	if err := s.lines.Save(ctx, line); err != nil {
		fields["description"] = "failed to save statement line"
		logrus.WithContext(ctx).WithError(err).WithFields(fields).Error(err.Error())
		return err
	}

	fields["description"] = "provider statement line recorded"
	logrus.WithContext(ctx).WithFields(fields).Info("RecordStatementLine")
	return nil
}

// ReconcileReport summarizes one Reconcile run.
type ReconcileReport struct {
	Checked           int `json:"checked"`
	Settled           int `json:"settled"`
	Failed            int `json:"failed"`
	AmountMismatches  int `json:"amount_mismatches"`
	MissingStatements int `json:"missing_statements"`
}

// Reconcile compares every pending deposit (older than the grace period)
// against what the provider reported for it:
//   - a matching line with the right amount and status "settled" -> the
//     transaction is marked settled.
//   - a matching line with the right amount and any other status -> marked
//     failed (the provider is telling us this charge did not go through).
//   - a matching line with the WRONG amount -> flagged as a discrepancy,
//     left pending for a human to look at (too ambiguous to resolve alone).
//   - no matching line at all, past the grace period -> flagged as a
//     "missing_statement" discrepancy, left pending (the webhook may still
//     be coming, or may be lost - resolving that needs a human or a
//     provider-side status query this project doesn't build).
//
// Known gap: this only checks the ledger->provider direction. A statement
// line matching no transaction at all (an "orphan" webhook) is not
// flagged - that's the reverse direction of the same comparison and isn't
// built here.
//
// Idempotent: running this repeatedly does not create duplicate unresolved
// discrepancies or re-settle an already-settled transaction (settled/failed
// transactions no longer show up in FindPendingDeposits).
func (s *Service) Reconcile(ctx context.Context) (*ReconcileReport, error) {
	report := &ReconcileReport{}
	cutoff := time.Now().UTC().Add(-s.gracePeriod)

	pending, err := s.transactions.FindPendingDeposits(ctx, cutoff)
	if err != nil {
		return nil, err
	}

	for _, txn := range pending {
		report.Checked++
		fields := logrus.Fields{
			"transaction_id": txn.ID, "idempotency_key": txn.IdempotencyKey, "amount": txn.Amount,
		}

		line, err := s.lines.FindByProviderReference(ctx, txn.IdempotencyKey)
		if errors.Is(err, ErrStatementLineNotFound) {
			if derr := s.flagDiscrepancy(ctx, txn.ID, txn.IdempotencyKey, "missing_statement", fields); derr != nil {
				return nil, derr
			}
			report.MissingStatements++
			continue
		}
		if err != nil {
			fields["description"] = "failed to load statement line"
			logrus.WithContext(ctx).WithError(err).WithFields(fields).Error(err.Error())
			return nil, err
		}

		if line.Amount() != txn.Amount {
			fields["reported_amount"] = line.Amount()
			if derr := s.flagDiscrepancy(ctx, txn.ID, txn.IdempotencyKey, "amount_mismatch", fields); derr != nil {
				return nil, derr
			}
			report.AmountMismatches++
			continue
		}

		if line.Status() == "settled" {
			if err := s.settler.MarkSettled(ctx, txn.ID); err != nil {
				fields["description"] = "failed to mark settled"
				logrus.WithContext(ctx).WithError(err).WithFields(fields).Error(err.Error())
				return nil, err
			}
			fields["description"] = "transaction settled"
			logrus.WithContext(ctx).WithFields(fields).Info("Reconcile")
			report.Settled++
			continue
		}

		if err := s.settler.MarkFailed(ctx, txn.ID); err != nil {
			fields["description"] = "failed to mark failed"
			logrus.WithContext(ctx).WithError(err).WithFields(fields).Error(err.Error())
			return nil, err
		}
		fields["description"] = "transaction failed"
		fields["provider_status"] = line.Status()
		logrus.WithContext(ctx).WithFields(fields).Warn("Reconcile")
		report.Failed++
	}

	logrus.WithContext(ctx).WithFields(logrus.Fields{
		"description": "reconciliation run complete",
		"checked": report.Checked, "settled": report.Settled, "failed": report.Failed,
		"amount_mismatches": report.AmountMismatches, "missing_statements": report.MissingStatements,
	}).Info("Reconcile")

	return report, nil
}

func (s *Service) flagDiscrepancy(ctx context.Context, transactionID uuid.UUID, providerReference, discrepancyType string, fields logrus.Fields) error {
	d, err := NewDiscrepancy(transactionID, providerReference, discrepancyType)
	if err != nil {
		return err
	}
	if err := s.discrepancies.Save(ctx, d); err != nil {
		fields["description"] = "failed to save discrepancy"
		logrus.WithContext(ctx).WithError(err).WithFields(fields).Error(err.Error())
		return err
	}
	fields["description"] = "discrepancy flagged"
	fields["discrepancy_type"] = discrepancyType
	logrus.WithContext(ctx).WithFields(fields).Warn("Reconcile")
	return nil
}
