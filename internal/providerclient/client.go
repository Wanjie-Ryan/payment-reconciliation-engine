package providerclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/Wanjie-Ryan/payment-reconciliation-engine/internal/ledger"
)

// Client implements ledger.PaymentProviderClient over HTTP against the mock
// payment provider.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

func New(baseURL string) *Client {
	return &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

type chargeRequest struct {
	Reference string `json:"reference"`
	Amount    int64  `json:"amount"`
	Currency  string `json:"currency"`
	// Behavior lets a caller (ultimately the ledger's own API client, via
	// the deposit request) ask the mock provider to misbehave on purpose -
	// "duplicate", "missing", "mismatch", "declined", or "" for normal.
	// Only meaningful against the mock provider.
	Behavior string `json:"behavior,omitempty"`
}

func (c *Client) InitiateCharge(ctx context.Context, reference string, amount ledger.Money, behavior string) error {
	fields := logrus.Fields{"provider_reference": reference, "amount": amount.Amount(), "currency": amount.Currency(), "behavior": behavior}

	body, err := json.Marshal(chargeRequest{Reference: reference, Amount: amount.Amount(), Currency: amount.Currency(), Behavior: behavior})
	if err != nil {
		fields["description"] = "failed to marshal charge request"
		logrus.WithContext(ctx).WithError(err).WithFields(fields).Error(err.Error())
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/charges", bytes.NewReader(body))
	if err != nil {
		fields["description"] = "failed to build charge request"
		logrus.WithContext(ctx).WithError(err).WithFields(fields).Error(err.Error())
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		fields["description"] = "provider unreachable"
		logrus.WithContext(ctx).WithError(err).WithFields(fields).Warn(err.Error())
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		err := fmt.Errorf("provider returned status %d", resp.StatusCode)
		fields["description"] = "provider rejected charge"
		logrus.WithContext(ctx).WithError(err).WithFields(fields).Warn(err.Error())
		return err
	}

	fields["description"] = "charge initiated"
	logrus.WithContext(ctx).WithFields(fields).Info("InitiateCharge")
	return nil
}
