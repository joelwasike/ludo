package models

import "time"

// TransactionType represents the kind of wallet transaction.
type TransactionType string

const (
	TxDeposit  TransactionType = "deposit"
	TxWithdraw TransactionType = "withdrawal"
	TxBetLock  TransactionType = "bet_lock"
	TxBetWin   TransactionType = "bet_win"
	TxBetRefund TransactionType = "bet_refund"
	TxHouseFee TransactionType = "house_fee"
)

// TransactionStatus tracks the lifecycle of a transaction.
type TransactionStatus string

const (
	TxStatusPending   TransactionStatus = "pending"
	TxStatusCompleted TransactionStatus = "completed"
	TxStatusFailed    TransactionStatus = "failed"
)

// Wallet holds a player's balance.
type Wallet struct {
	ID        int64     `json:"id"`
	PlayerID  string    `json:"player_id"`
	Balance   float64   `json:"balance"`
	Currency  string    `json:"currency"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Transaction is an immutable audit record of a wallet operation.
type Transaction struct {
	ID          int64             `json:"id"`
	WalletID    int64             `json:"wallet_id"`
	Type        TransactionType   `json:"type"`
	Amount      float64           `json:"amount"`
	Reference   string            `json:"reference,omitempty"`
	Description string            `json:"description"`
	Status      TransactionStatus `json:"status"`
	CreatedAt   time.Time         `json:"created_at"`
}

// StakeTiersKES are the available bet amounts.
var StakeTiersKES = []float64{20, 50, 150, 500, 1000, 5000}

const (
	MinBetAmount    = 20.0
	HouseFeePercent = 0.20
	KESToUSDRate    = 0.0077
)
