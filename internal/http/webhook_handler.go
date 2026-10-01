package http

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"

	"github.com/Wanjie-Ryan/payment-reconciliation-engine/internal/reconciliation"
)

// WebhookHandler translates inbound provider webhooks to reconciliation
// service calls and back. No business logic lives here.
type WebhookHandler struct {
	reconciliation *reconciliation.Service
}

func RegisterWebhookRoutes(e *echo.Echo, svc *reconciliation.Service) {
	h := &WebhookHandler{reconciliation: svc}
	e.POST("/webhooks/mock-provider", h.mockProviderWebhook)
}

type MockProviderWebhookRequest struct {
	ProviderReference string    `json:"provider_reference"`
	Amount            int64     `json:"amount"`
	Status            string    `json:"status"`
	ReportedAt        time.Time `json:"reported_at"`
}

func (h *WebhookHandler) mockProviderWebhook(c echo.Context) error {
	var req MockProviderWebhookRequest
	if err := c.Bind(&req); err != nil {
		logrus.WithContext(c.Request().Context()).WithError(err).WithFields(logrus.Fields{
			"description": "invalid webhook payload",
		}).Warn(err.Error())
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	if err := h.reconciliation.RecordStatementLine(c.Request().Context(), req.ProviderReference, req.Amount, req.Status, req.ReportedAt); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid webhook"})
	}

	// Always 200 on a structurally valid webhook, even a duplicate - the
	// provider must never see an error and retry forever over something we
	// already have (or deliberately ignored).
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}
