package http

import (
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"

	"github.com/Wanjie-Ryan/payment-reconciliation-engine/internal/ledger"
)

// handler translates HTTP TO ledgerservice calls and back.

type Handler struct {
	ledger *ledger.LedgerService
}

func RegisterRoutes(e *echo.Echo, svc *ledger.LedgerService) {
	h := &Handler{ledger: svc}

	e.POST("/accounts", h.createAccount)
	e.GET("/accounts/:id", h.getAccount)
	e.POST("/deposits", h.deposit)
	e.POST("/withdrawals", h.withdraw)
	e.POST("/transfers", h.transfer)
}

type CreateAccountRequest struct {
	Owner    string `json:"owner"`
	Currency string `json:"currency"`
}

type AccountResponse struct {
	ID       uuid.UUID `json:"id"`
	Owner    string    `json:"owner"`
	Currency string    `json:"currency"`
	Balance  int64     `json:"balance"`
}

// singleAccountRequest is the body for deposits and withdrawals
type SingleAccountRequest struct {
	// AccountID is the account; money goes into (deposit) or out of (Withdrawal)
	AccountID uuid.UUID `json:"account_id"`
	Amount    int64     `json:"amount"`
	Currency  string    `json:"currency"`
}

type TransferRequest struct {
	FromAccountID uuid.UUID `json:"from_account_id"`
	ToAccountID   uuid.UUID `json:"to_account_id"`
	Amount        int64     `json:"amount"`
	Currency      string    `json:"currency"`
}

type EntryResponse struct {
	AccountID uuid.UUID `json:"account_id"`
	Amount    int64     `json:"amount"`
}

type TransactionResponse struct {
	ID             uuid.UUID       `json:"id"`
	Type           string          `json:"type"`
	Status         string          `json:"status"`
	IdempotencyKey string          `json:"idempotency_key"`
	Entries        []EntryResponse `json:"entries"`
	CreatedAt      time.Time       `json:"created_at"`
}

func toTransactionResponse(txn *ledger.Transaction) TransactionResponse {
	entries := make([]EntryResponse, 0, len(txn.Entries()))
	for _, e := range txn.Entries() {
		entries = append(entries, EntryResponse{AccountID: e.AccountID(), Amount: e.Amount().Amount()})
	}
	return TransactionResponse{
		ID:             txn.ID(),
		Type:           txn.Type(),
		Status:         string(txn.Status()),
		IdempotencyKey: txn.IdempotencyKey(),
		Entries:        entries,
		CreatedAt:      txn.CreatedAt(),
	}
}

// respondError maps domain errors to status codes. The service has already logged the failure

func respondError(c echo.Context, err error) error {
	switch {
	case errors.Is(err, ledger.ErrInvalid):
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, ledger.ErrAccountNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
	default:
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})

	}
}

func badRequest(c echo.Context, description string, err error) error {
	logrus.WithContext(c.Request().Context()).WithError(err).WithFields(logrus.Fields{
		"description": description,
	}).Warn(err.Error())
	return c.JSON(http.StatusBadRequest, map[string]string{"error": description})
}

func idempotencyKey(c echo.Context) string {
	return c.Request().Header.Get("Idempotency-Key")
}

func (h *Handler) createAccount(c echo.Context) error {

	var req CreateAccountRequest

	if err := c.Bind(&req); err != nil {
		return badRequest(c, "invalid request body", err)
	}

	account, err := h.ledger.CreateAccount(c.Request().Context(), req.Owner, req.Currency)

	if err != nil {
		return respondError(c, err)
	}

	return c.JSON(http.StatusCreated, AccountResponse{
		ID: account.ID(), Owner: account.Owner(), Currency: account.Currency(), Balance: 0,
	})

}

func (h *Handler) getAccount(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))

	if err != nil {
		return badRequest(c, "invalid account id", err)
	}

	account, balance, err := h.ledger.GetAccount(c.Request().Context(), id)
	if err != nil {
		return respondError(c, err)
	}

	return c.JSON(http.StatusOK, AccountResponse{
		ID: account.ID(), Owner: account.Owner(), Currency: account.Currency(), Balance: balance.Amount(),
	})

}

func (h *Handler) deposit(c echo.Context) error {
	var req SingleAccountRequest

	if err := c.Bind(&req); err != nil {
		return badRequest(c, "invalid request body", err)
	}

	newCurrency := strings.ToUpper(req.Currency)

	log.Printf(newCurrency)

	amount, err := ledger.NewMoney(req.Amount, req.Currency)

	if err != nil {
		return respondError(c, err)
	}

	txn, err := h.ledger.Deposit(c.Request().Context(), req.AccountID, amount, idempotencyKey(c))

	if err != nil {
		return respondError(c, err)
	}

	return c.JSON(http.StatusCreated, toTransactionResponse(txn))

}

func (h *Handler) withdraw(c echo.Context) error {
	var req SingleAccountRequest

	if err := c.Bind(&req); err != nil {
		return badRequest(c, "invalid request body", err)
	}

	newCurrency := strings.ToUpper(req.Currency)

	log.Printf(newCurrency)

	amount, err := ledger.NewMoney(req.Amount, req.Currency)

	if err != nil {
		return respondError(c, err)
	}

	txn, err := h.ledger.Withdraw(c.Request().Context(), req.AccountID, amount, idempotencyKey(c))

	if err != nil {
		return respondError(c, err)
	}

	return c.JSON(http.StatusCreated, toTransactionResponse(txn))

}

func (h *Handler) transfer(c echo.Context) error {
	var req TransferRequest

	if err := c.Bind(&req); err != nil {
		return badRequest(c, "invalid request body", err)
	}

	amount, err := ledger.NewMoney(req.Amount, req.Currency)

	if err != nil {
		return respondError(c, err)
	}

	txn, err := h.ledger.Transfer(c.Request().Context(), req.FromAccountID, req.ToAccountID, amount, idempotencyKey(c))

	if err != nil {
		return respondError(c, err)
	}

	return c.JSON(http.StatusCreated, toTransactionResponse(txn))

}
