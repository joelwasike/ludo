package payment

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// Service handles external payment API calls (M-Pesa, USDT/Solana).
type Service struct {
	MpesaBaseURL    string
	USDTBaseURL     string
	MerchantEmail   string
	MerchantPass    string
	CallbackBaseURL string
	httpClient      *http.Client

	mpesaToken    string
	mpesaTokenExp time.Time
	usdtToken     string
	usdtTokenExp  time.Time
	mu            sync.Mutex
}

// DepositResult holds the result from initiating a deposit.
type DepositResult struct {
	Reference string `json:"reference"`
	PageURL   string `json:"page_url,omitempty"`
}

// NewService creates a new payment service.
func NewService(callbackBaseURL string) *Service {
	return &Service{
		MpesaBaseURL:    "https://card-api.theliberec.com",
		USDTBaseURL:     "https://api.swapuzi.com",
		MerchantEmail:   "chess@gmail.com",
		MerchantPass:    "joelwasike",
		CallbackBaseURL: callbackBaseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// --- Auth ---

func (s *Service) getMpesaToken() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.mpesaToken != "" && time.Now().Before(s.mpesaTokenExp) {
		return s.mpesaToken, nil
	}

	url := s.MpesaBaseURL + "/api/v1/merchants/login"
	body, _ := json.Marshal(map[string]string{
		"email":    s.MerchantEmail,
		"password": s.MerchantPass,
	})

	resp, err := s.httpClient.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("mpesa login request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("mpesa login failed (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("mpesa login parse error: %w", err)
	}

	s.mpesaToken = result.Token
	s.mpesaTokenExp = time.Now().Add(50 * time.Minute)
	return s.mpesaToken, nil
}

func (s *Service) getUSDTToken() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.usdtToken != "" && time.Now().Before(s.usdtTokenExp) {
		return s.usdtToken, nil
	}

	url := s.USDTBaseURL + "/merchants/login"
	body, _ := json.Marshal(map[string]string{
		"email":    s.MerchantEmail,
		"password": s.MerchantPass,
	})

	resp, err := s.httpClient.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("usdt login request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("usdt login failed (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("usdt login parse error: %w", err)
	}

	s.usdtToken = result.Token
	s.usdtTokenExp = time.Now().Add(50 * time.Minute)
	return s.usdtToken, nil
}

// --- HTTP helpers with token retry ---

func (s *Service) doMpesaRequest(method, url string, reqBody []byte) ([]byte, error) {
	for attempt := 0; attempt < 2; attempt++ {
		token, err := s.getMpesaToken()
		if err != nil {
			return nil, fmt.Errorf("failed to authenticate with M-Pesa: %w", err)
		}

		req, _ := http.NewRequest(method, url, bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := s.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("M-Pesa API request failed: %w", err)
		}
		defer resp.Body.Close()

		respBody, _ := io.ReadAll(resp.Body)

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return respBody, nil
		}

		if resp.StatusCode == 401 && attempt == 0 {
			slog.Warn("M-Pesa returned 401, retrying with fresh token")
			s.mu.Lock()
			s.mpesaToken = ""
			s.mu.Unlock()
			continue
		}

		return nil, fmt.Errorf("M-Pesa API error (status %d): %s", resp.StatusCode, string(respBody))
	}
	return nil, fmt.Errorf("M-Pesa API request failed after retry")
}

func (s *Service) doUSDTRequest(method, url string, reqBody []byte) ([]byte, error) {
	for attempt := 0; attempt < 2; attempt++ {
		token, err := s.getUSDTToken()
		if err != nil {
			return nil, fmt.Errorf("failed to authenticate with USDT API: %w", err)
		}

		req, _ := http.NewRequest(method, url, bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := s.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("USDT API request failed: %w", err)
		}
		defer resp.Body.Close()

		respBody, _ := io.ReadAll(resp.Body)

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return respBody, nil
		}

		if resp.StatusCode == 401 && attempt == 0 {
			slog.Warn("USDT API returned 401, retrying with fresh token")
			s.mu.Lock()
			s.usdtToken = ""
			s.mu.Unlock()
			continue
		}

		return nil, fmt.Errorf("USDT API error (status %d): %s", resp.StatusCode, string(respBody))
	}
	return nil, fmt.Errorf("USDT API request failed after retry")
}

// --- M-Pesa Deposit (STK Push) ---

func (s *Service) InitiateMpesaDeposit(playerID, phone, displayName string, amount float64) (*DepositResult, error) {
	orderID := fmt.Sprintf("LUDO-MPESA-%s-%d", playerID, time.Now().UnixMilli())

	reqBody, _ := json.Marshal(map[string]interface{}{
		"amount":              fmt.Sprintf("%.0f", amount),
		"currency":            "KES",
		"description":         fmt.Sprintf("Ludo deposit for %s", displayName),
		"customer_phone":      phone,
		"customer_first_name": displayName,
		"customer_last_name":  "Player",
		"customer_email":      "player@ludo.app",
		"callback_url":        s.CallbackBaseURL + "/mpesa",
		"order_id":            orderID,
	})

	url := s.MpesaBaseURL + "/api/v1/transactions/mpesa"
	if _, err := s.doMpesaRequest("POST", url, reqBody); err != nil {
		return nil, err
	}

	slog.Info("M-Pesa deposit initiated", "order", orderID, "player", playerID, "amount", amount)
	return &DepositResult{Reference: orderID}, nil
}

// --- USDT/Solana Deposit ---

func (s *Service) InitiateUSDTDeposit(playerID, displayName string, amount float64) (*DepositResult, error) {
	depositID := fmt.Sprintf("LUDO-USDT-%s-%d", playerID, time.Now().UnixMilli())

	reqBody, _ := json.Marshal(map[string]interface{}{
		"expected_amount": amount,
		"webhook_url":     s.CallbackBaseURL + "/usdt",
		"notes":           fmt.Sprintf("Ludo deposit for %s", displayName),
		"deposit_id":      depositID,
	})

	url := s.USDTBaseURL + "/merchants/solana/deposit/initiate"
	respBody, err := s.doUSDTRequest("POST", url, reqBody)
	if err != nil {
		return nil, err
	}

	var depositResp struct {
		PageURL string `json:"page_url"`
	}
	if err := json.Unmarshal(respBody, &depositResp); err != nil {
		return nil, fmt.Errorf("USDT response parse error: %w", err)
	}

	slog.Info("USDT deposit initiated", "id", depositID, "player", playerID, "amount", amount)
	return &DepositResult{Reference: depositID, PageURL: depositResp.PageURL}, nil
}

// --- M-Pesa Withdrawal (B2C) ---

func (s *Service) InitiateMpesaWithdraw(playerID string, amount float64, phone string) (string, error) {
	orderID := fmt.Sprintf("LUDO-B2C-%s-%d", playerID, time.Now().UnixMilli())

	reqBody, _ := json.Marshal(map[string]interface{}{
		"amount":       fmt.Sprintf("%.0f", amount),
		"phone_number": phone,
		"description":  "Ludo withdrawal payment",
		"remarks":      "Withdrawal payment",
		"order_id":     orderID,
		"callback_url": s.CallbackBaseURL + "/mpesa",
	})

	url := s.MpesaBaseURL + "/api/v1/transactions/mpesa/b2c"
	if _, err := s.doMpesaRequest("POST", url, reqBody); err != nil {
		return "", err
	}

	slog.Info("M-Pesa B2C initiated", "order", orderID, "player", playerID, "amount", amount)
	return orderID, nil
}

// --- USDT/Solana Withdrawal ---

func (s *Service) InitiateUSDTWithdraw(playerID string, amount float64, walletAddress string) (string, error) {
	reqBody, _ := json.Marshal(map[string]interface{}{
		"destination_address": walletAddress,
		"amount":              amount,
	})

	url := s.USDTBaseURL + "/merchants/solana/withdraw"
	respBody, err := s.doUSDTRequest("POST", url, reqBody)
	if err != nil {
		return "", err
	}

	var withdrawResp struct {
		TxID string `json:"tx_id"`
	}
	if err := json.Unmarshal(respBody, &withdrawResp); err != nil {
		return "", fmt.Errorf("USDT withdraw response parse error: %w", err)
	}

	slog.Info("USDT withdraw completed", "player", playerID, "amount", amount, "tx", withdrawResp.TxID)
	return withdrawResp.TxID, nil
}
