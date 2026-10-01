package main

import (
	"bytes"
	"encoding/json"
	"math/rand"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"
)

func logsInit() {
	logrus.SetFormatter(&logrus.JSONFormatter{})
}

func loggingMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {

		start := time.Now()
		err := next(c)

		logrus.WithContext(c.Request().Context()).WithFields(logrus.Fields{
			"description": "request handled",
			"method":      c.Request().Method,
			"path":        c.Path(),
			"status":      c.Response().Status,
			"duration_ms": time.Since(start).Milliseconds(),
		}).Info("request handled")

		return err
	}
}

type chargeRequest struct {
	Reference string `json:"reference"`
	Amount    int64  `json:"amount"`
	Currency  string `json:"currency"`
	// Behavior controls how the webhook gets delivered: "normal" (default),
	// "duplicate", "missing", "mismatch", or "declined".
	Behavior string `json:"behavior"`
}

type webhookPayload struct {
	ProviderReference string    `json:"provider_reference"`
	Amount            int64     `json:"amount"`
	Currency          string    `json:"currency"`
	Status            string    `json:"status"`
	ReportedAt        time.Time `json:"reported_at"`
}

func deliverWebhook(webhookURL string, payload webhookPayload) {
	fields := logrus.Fields{"provider_reference": payload.ProviderReference, "webhook_url": webhookURL}

	body, err := json.Marshal(payload)
	if err != nil {
		fields["description"] = "failed to marshal webhook payload"
		logrus.WithError(err).WithFields(fields).Error(err.Error())
		return
	}

	resp, err := http.Post(webhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		fields["description"] = "webhook delivery failed"
		logrus.WithError(err).WithFields(fields).Warn(err.Error())
		return
	}
	defer resp.Body.Close()

	fields["description"] = "webhook delivered"
	fields["status_code"] = resp.StatusCode
	logrus.WithFields(fields).Info("deliverWebhook")
}

// simulateDelivery runs the configured misbehavior for one charge, after a
// short random delay matching real async settlement (an M-Pesa STK push
// callback typically lands a few seconds after the request, not instantly).
func simulateDelivery(webhookURL string, req chargeRequest) {
	delay := time.Duration(1000+rand.Intn(2000)) * time.Millisecond
	time.Sleep(delay)

	fields := logrus.Fields{"provider_reference": req.Reference, "behavior": req.Behavior}

	switch req.Behavior {
	case "missing":
		fields["description"] = "simulated missing webhook - not delivering"
		logrus.WithFields(fields).Warn("simulateDelivery")

	case "mismatch":
		payload := webhookPayload{
			ProviderReference: req.Reference,
			Amount:            req.Amount + 1, // deliberately wrong
			Currency:          req.Currency,
			Status:            "settled",
			ReportedAt:        time.Now().UTC(),
		}
		fields["description"] = "simulated amount mismatch"
		fields["requested_amount"] = req.Amount
		fields["reported_amount"] = payload.Amount
		logrus.WithFields(fields).Warn("simulateDelivery")
		deliverWebhook(webhookURL, payload)

	case "duplicate":
		payload := webhookPayload{
			ProviderReference: req.Reference,
			Amount:            req.Amount,
			Currency:          req.Currency,
			Status:            "settled",
			ReportedAt:        time.Now().UTC(),
		}
		fields["description"] = "simulated duplicate delivery"
		logrus.WithFields(fields).Warn("simulateDelivery")
		deliverWebhook(webhookURL, payload)
		deliverWebhook(webhookURL, payload)

	case "declined":
		payload := webhookPayload{
			ProviderReference: req.Reference,
			Amount:            req.Amount,
			Currency:          req.Currency,
			Status:            "failed",
			ReportedAt:        time.Now().UTC(),
		}
		fields["description"] = "simulated provider decline"
		logrus.WithFields(fields).Warn("simulateDelivery")
		deliverWebhook(webhookURL, payload)

	default:
		payload := webhookPayload{
			ProviderReference: req.Reference,
			Amount:            req.Amount,
			Currency:          req.Currency,
			Status:            "settled",
			ReportedAt:        time.Now().UTC(),
		}
		deliverWebhook(webhookURL, payload)
	}
}

func main() {

	logsInit()

	if err := godotenv.Load(); err != nil {

		logrus.WithFields(logrus.Fields{
			"description": "no .env file found, relying on process environment",
		}).Info("godotenv")
	}

	webhookURL := os.Getenv("LEDGER_WEBHOOK_URL")

	e := echo.New()
	e.HideBanner = true
	e.Use(loggingMiddleware)

	e.GET("/health", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok", "service": "mock-payment-provider"})
	})

	e.POST("/charges", func(c echo.Context) error {
		var req chargeRequest
		if err := c.Bind(&req); err != nil {
			logrus.WithContext(c.Request().Context()).WithError(err).WithFields(logrus.Fields{
				"description": "invalid charge request",
			}).Warn(err.Error())
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		}
		if req.Reference == "" {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "reference is required"})
		}
		if req.Behavior == "" {
			req.Behavior = "normal"
		}

		logrus.WithContext(c.Request().Context()).WithFields(logrus.Fields{
			"description":        "charge accepted",
			"provider_reference": req.Reference,
			"amount":             req.Amount,
			"currency":           req.Currency,
			"behavior":           req.Behavior,
		}).Info("charges")

		go simulateDelivery(webhookURL, req)

		return c.JSON(http.StatusAccepted, map[string]string{"reference": req.Reference, "status": "pending"})
	})

	port := os.Getenv("MOCK_PROVIDER_PORT")
	if port == "" {
		port = "8080"
	}

	addr := ":" + port

	logrus.WithFields(logrus.Fields{
		"description": "starting mock payment provider",
		"addr":        addr,
		"webhook_url": webhookURL,
	}).Info("main")

	if err := e.Start(addr); err != nil {

		logrus.WithError(err).WithFields(logrus.Fields{
			"description": "server stopped",
		}).Error(err.Error())
	}
}
