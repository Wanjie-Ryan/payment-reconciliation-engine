package http

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/Wanjie-Ryan/payment-reconciliation-engine/internal/ledger"
	"github.com/Wanjie-Ryan/payment-reconciliation-engine/internal/reconciliation"
)

// AdminHandler exposes operational endpoints that aren't part of the public
// ledger API: triggering a replay or a reconciliation run by hand. No
// business logic lives here - it only calls the two services and returns
// their report.
type AdminHandler struct {
	replay         *ledger.ReplayService
	reconciliation *reconciliation.Service
}

func RegisterAdminRoutes(e *echo.Echo, replay *ledger.ReplayService, reconciliation *reconciliation.Service) {
	h := &AdminHandler{replay: replay, reconciliation: reconciliation}
	e.GET("/admin/replay", h.runReplay)
	e.POST("/admin/reconcile", h.runReconcile)
}

// runReplay rebuilds every account's balance from the event log alone and
// compares it against the live balance - proof the event log is sufficient
// on its own to reconstruct current state.
func (h *AdminHandler) runReplay(c echo.Context) error {
	report, err := h.replay.Replay(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "replay failed"})
	}
	return c.JSON(http.StatusOK, report)
}

// runReconcile compares pending deposits against what the mock provider
// reported for them, settling, failing, or flagging a discrepancy on each.
func (h *AdminHandler) runReconcile(c echo.Context) error {
	report, err := h.reconciliation.Reconcile(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "reconciliation failed"})
	}
	return c.JSON(http.StatusOK, report)
}
