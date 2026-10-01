package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sirupsen/logrus"

	"github.com/Wanjie-Ryan/payment-reconciliation-engine/internal/ledger"
)

// EventRepository implements ledger.EventRepository. It is read-only: the
// only write path is inside TransactionRepository.Save, atomic with the
// entries it describes.
type EventRepository struct {
	pool *pgxpool.Pool
}

func NewEventRepository(pool *pgxpool.Pool) *EventRepository {
	return &EventRepository{pool: pool}
}

func (r *EventRepository) FindAllOrdered(ctx context.Context) ([]ledger.Event, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, transaction_id, event_type, payload, created_at FROM events ORDER BY created_at ASC, id ASC`,
	)
	if err != nil {
		logrus.WithContext(ctx).WithError(err).WithFields(logrus.Fields{
			"description": "select from events failed",
		}).Error(err.Error())
		return nil, err
	}
	defer rows.Close()

	var events []ledger.Event
	for rows.Next() {
		var (
			id, transactionID uuid.UUID
			eventType         string
			payloadJSON       []byte
			createdAt         time.Time
		)
		if err := rows.Scan(&id, &transactionID, &eventType, &payloadJSON, &createdAt); err != nil {
			logrus.WithContext(ctx).WithError(err).WithFields(logrus.Fields{
				"description": "scan event row failed",
			}).Error(err.Error())
			return nil, err
		}

		var payload ledger.TransactionPostedPayload
		if err := json.Unmarshal(payloadJSON, &payload); err != nil {
			logrus.WithContext(ctx).WithError(err).WithFields(logrus.Fields{
				"description": "unmarshal event payload failed",
				"event_id":    id,
			}).Error(err.Error())
			return nil, err
		}

		events = append(events, ledger.Event{
			ID: id, TransactionID: transactionID, EventType: eventType, Payload: payload, CreatedAt: createdAt,
		})
	}
	if err := rows.Err(); err != nil {
		logrus.WithContext(ctx).WithError(err).WithFields(logrus.Fields{
			"description": "iterate events failed",
		}).Error(err.Error())
		return nil, err
	}

	return events, nil
}
