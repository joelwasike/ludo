package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/ludo/server/internal/payment"
	"github.com/ludo/server/internal/wallet"
	"github.com/ludo/server/pkg/models"
)

// WalletHandler handles wallet and payment HTTP endpoints.
type WalletHandler struct {
	walletSvc  *wallet.Service
	paymentSvc *payment.Service
}

// NewWalletHandler creates a new wallet handler.
func NewWalletHandler(ws *wallet.Service, ps *payment.Service) *WalletHandler {
	return &WalletHandler{walletSvc: ws, paymentSvc: ps}
}

// --- Wallet endpoints (require player_id header) ---

func (h *WalletHandler) HandleGetBalance(w http.ResponseWriter, r *http.Request) {
	playerID := r.Header.Get("X-Player-ID")
	if playerID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing player ID"})
		return
	}

	wlt, err := h.walletSvc.GetOrCreateWallet(playerID)
	if err != nil {
		slog.Error("get balance failed", "player", playerID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load wallet"})
		return
	}

	writeJSON(w, http.StatusOK, wlt)
}

func (h *WalletHandler) HandleGetTransactions(w http.ResponseWriter, r *http.Request) {
	playerID := r.Header.Get("X-Player-ID")
	if playerID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing player ID"})
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit < 1 || limit > 100 {
		limit = 20
	}

	txns, err := h.walletSvc.GetTransactions(playerID, limit, offset)
	if err != nil {
		slog.Error("get transactions failed", "player", playerID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load transactions"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"transactions": txns})
}

type depositRequest struct {
	Amount float64 `json:"amount"`
	Method string  `json:"method"` // "mpesa" or "usdt"
	Phone  string  `json:"phone"`  // required for mpesa
}

func (h *WalletHandler) HandleDeposit(w http.ResponseWriter, r *http.Request) {
	playerID := r.Header.Get("X-Player-ID")
	if playerID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing player ID"})
		return
	}

	var req depositRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if req.Amount <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "amount must be positive"})
		return
	}

	var result *payment.DepositResult
	var err error

	switch req.Method {
	case "mpesa":
		if req.Phone == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "phone number is required for M-Pesa"})
			return
		}
		result, err = h.paymentSvc.InitiateMpesaDeposit(playerID, req.Phone, "Player", req.Amount)
	case "usdt":
		result, err = h.paymentSvc.InitiateUSDTDeposit(playerID, "Player", req.Amount)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported method, use 'mpesa' or 'usdt'"})
		return
	}

	if err != nil {
		slog.Error("deposit initiation failed", "player", playerID, "error", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "payment service temporarily unavailable"})
		return
	}

	// Store pending transaction for callback matching
	_ = h.walletSvc.CreatePendingTransaction(playerID, models.TxDeposit, req.Amount, result.Reference, "Deposit pending: "+req.Method)

	resp := map[string]interface{}{
		"message":   "Deposit initiated",
		"reference": result.Reference,
	}
	if result.PageURL != "" {
		resp["page_url"] = result.PageURL
	}
	writeJSON(w, http.StatusOK, resp)
}

type withdrawRequest struct {
	Amount        float64 `json:"amount"`
	Method        string  `json:"method"`         // "mpesa" or "usdt"
	Phone         string  `json:"phone"`           // required for mpesa
	WalletAddress string  `json:"wallet_address"`  // required for usdt
}

func (h *WalletHandler) HandleWithdraw(w http.ResponseWriter, r *http.Request) {
	playerID := r.Header.Get("X-Player-ID")
	if playerID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing player ID"})
		return
	}

	var req withdrawRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if req.Amount <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "amount must be positive"})
		return
	}

	var reference string
	var err error

	switch req.Method {
	case "mpesa":
		if req.Phone == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "phone number is required for M-Pesa withdrawal"})
			return
		}
		// Pre-deduct
		if err := h.walletSvc.Withdraw(playerID, req.Amount); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		reference, err = h.paymentSvc.InitiateMpesaWithdraw(playerID, req.Amount, req.Phone)
		if err != nil {
			// Reverse the deduction
			_ = h.walletSvc.Deposit(playerID, req.Amount, fmt.Sprintf("REVERSE-%s-%d", playerID, time.Now().UnixMilli()))
			slog.Error("M-Pesa B2C failed", "player", playerID, "error", err)
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "M-Pesa withdrawal temporarily unavailable"})
			return
		}
		_ = h.walletSvc.CreatePendingTransaction(playerID, models.TxWithdraw, -req.Amount, reference, "M-Pesa withdrawal pending")

	case "usdt":
		if req.WalletAddress == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "wallet address is required for USDT withdrawal"})
			return
		}
		if err := h.walletSvc.Withdraw(playerID, req.Amount); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		reference, err = h.paymentSvc.InitiateUSDTWithdraw(playerID, req.Amount, req.WalletAddress)
		if err != nil {
			_ = h.walletSvc.Deposit(playerID, req.Amount, fmt.Sprintf("REVERSE-%s-%d", playerID, time.Now().UnixMilli()))
			slog.Error("USDT withdrawal failed", "player", playerID, "error", err)
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "USDT withdrawal temporarily unavailable"})
			return
		}

	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported method"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message":   "Withdrawal initiated",
		"reference": reference,
	})
}

func (h *WalletHandler) HandleStakeTiers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"tiers":    models.StakeTiersKES,
		"currency": "KES",
		"min_bet":  models.MinBetAmount,
	})
}

// --- Payment Callbacks (no auth) ---

func (h *WalletHandler) HandleMpesaCallback(w http.ResponseWriter, r *http.Request) {
	var raw map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		slog.Error("M-Pesa callback parse error", "error", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid payload"})
		return
	}

	slog.Info("M-Pesa callback received", "payload", raw)

	merchantOrderID, _ := raw["merchant_order_id"].(string)
	status, _ := raw["status"].(string)

	var amount float64
	switch v := raw["amount"].(type) {
	case float64:
		amount = v
	case string:
		fmt.Sscanf(v, "%f", &amount)
	}

	if status != "COMPLETED" {
		// Failed — if this was a withdrawal, refund
		_ = h.walletSvc.RefundFailedWithdraw(merchantOrderID)
		_ = h.walletSvc.FailPendingTransaction(merchantOrderID)
		writeJSON(w, http.StatusOK, map[string]string{"message": "callback processed"})
		return
	}

	// Completed — deposit or withdrawal
	if err := h.walletSvc.CompletePendingDeposit(merchantOrderID, amount); err != nil {
		// Not a deposit — try completing a withdrawal
		_ = h.walletSvc.CompletePendingWithdraw(merchantOrderID)
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "callback processed"})
}

func (h *WalletHandler) HandleUSDTCallback(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Event             string  `json:"event"`
		MerchantDepositID string  `json:"merchant_deposit_id"`
		PaymentStatus     string  `json:"payment_status"`
		Status            string  `json:"status"`
		Amount            float64 `json:"amount"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		slog.Error("USDT callback parse error", "error", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid payload"})
		return
	}

	slog.Info("USDT callback received",
		"event", payload.Event, "deposit_id", payload.MerchantDepositID,
		"payment_status", payload.PaymentStatus, "status", payload.Status, "amount", payload.Amount)

	isConfirmed := payload.Event == "deposit.confirmed" &&
		payload.PaymentStatus == "paid" &&
		payload.Status == "confirmed"

	if !isConfirmed {
		_ = h.walletSvc.FailPendingTransaction(payload.MerchantDepositID)
		writeJSON(w, http.StatusOK, map[string]string{"message": "callback processed"})
		return
	}

	if err := h.walletSvc.CompletePendingDeposit(payload.MerchantDepositID, payload.Amount); err != nil {
		slog.Error("USDT deposit completion failed", "ref", payload.MerchantDepositID, "error", err)
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "callback processed"})
}
