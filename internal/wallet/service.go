package wallet

import (
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/ludo/server/pkg/models"
)

// Service handles wallet operations with transactional guarantees.
type Service struct {
	db *sql.DB
}

// NewService creates a new wallet service.
func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

// GetOrCreateWallet returns the wallet for a player, creating one if needed.
func (s *Service) GetOrCreateWallet(playerID string) (*models.Wallet, error) {
	var w models.Wallet
	err := s.db.QueryRow(`SELECT id, player_id, balance, currency, created_at, updated_at FROM wallets WHERE player_id = ?`, playerID).
		Scan(&w.ID, &w.PlayerID, &w.Balance, &w.Currency, &w.CreatedAt, &w.UpdatedAt)
	if err == nil {
		return &w, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}

	now := time.Now()
	res, err := s.db.Exec(`INSERT INTO wallets (player_id, balance, currency, created_at, updated_at) VALUES (?, 0, 'KES', ?, ?)`,
		playerID, now, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &models.Wallet{
		ID: id, PlayerID: playerID, Balance: 0, Currency: "KES",
		CreatedAt: now, UpdatedAt: now,
	}, nil
}

// GetBalance returns the current balance.
func (s *Service) GetBalance(playerID string) (float64, error) {
	w, err := s.GetOrCreateWallet(playerID)
	if err != nil {
		return 0, err
	}
	return w.Balance, nil
}

// LockBet deducts a bet amount from the player's wallet.
func (s *Service) LockBet(playerID string, amount float64) error {
	if amount < models.MinBetAmount {
		return fmt.Errorf("minimum bet is %.0f KES", models.MinBetAmount)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var walletID int64
	var balance float64
	if err := tx.QueryRow(`SELECT id, balance FROM wallets WHERE player_id = ?`, playerID).Scan(&walletID, &balance); err != nil {
		return fmt.Errorf("wallet not found: %w", err)
	}

	if balance < amount {
		return fmt.Errorf("insufficient balance")
	}

	if _, err := tx.Exec(`UPDATE wallets SET balance = balance - ?, updated_at = ? WHERE id = ?`, amount, time.Now(), walletID); err != nil {
		return err
	}

	if _, err := tx.Exec(`INSERT INTO transactions (wallet_id, type, amount, description, status, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		walletID, models.TxBetLock, -amount, fmt.Sprintf("Bet locked: %.2f KES", amount), models.TxStatusCompleted, time.Now()); err != nil {
		return err
	}

	return tx.Commit()
}

// UnlockBet refunds a locked bet (e.g. player leaves before game starts).
func (s *Service) UnlockBet(playerID string, amount float64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var walletID int64
	if err := tx.QueryRow(`SELECT id FROM wallets WHERE player_id = ?`, playerID).Scan(&walletID); err != nil {
		return fmt.Errorf("wallet not found: %w", err)
	}

	if _, err := tx.Exec(`UPDATE wallets SET balance = balance + ?, updated_at = ? WHERE id = ?`, amount, time.Now(), walletID); err != nil {
		return err
	}

	if _, err := tx.Exec(`INSERT INTO transactions (wallet_id, type, amount, description, status, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		walletID, models.TxBetRefund, amount, fmt.Sprintf("Bet refund: %.2f KES", amount), models.TxStatusCompleted, time.Now()); err != nil {
		return err
	}

	return tx.Commit()
}

// SettleGame pays the winner 80% of the pot and records 20% house fee.
func (s *Service) SettleGame(roomCode string, winnerID string, totalPot float64) error {
	if totalPot <= 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	houseFee := totalPot * models.HouseFeePercent
	payout := totalPot - houseFee

	var walletID int64
	if err := tx.QueryRow(`SELECT id FROM wallets WHERE player_id = ?`, winnerID).Scan(&walletID); err != nil {
		return fmt.Errorf("winner wallet not found: %w", err)
	}

	if _, err := tx.Exec(`UPDATE wallets SET balance = balance + ?, updated_at = ? WHERE id = ?`, payout, time.Now(), walletID); err != nil {
		return err
	}

	now := time.Now()
	if _, err := tx.Exec(`INSERT INTO transactions (wallet_id, type, amount, description, status, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		walletID, models.TxBetWin, payout,
		fmt.Sprintf("Won room %s: %.2f KES (pot %.2f - 20%% fee)", roomCode, payout, totalPot),
		models.TxStatusCompleted, now); err != nil {
		return err
	}

	// Record house fee
	if _, err := tx.Exec(`INSERT INTO transactions (wallet_id, type, amount, description, status, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		walletID, models.TxHouseFee, houseFee,
		fmt.Sprintf("House fee room %s: %.2f KES", roomCode, houseFee),
		models.TxStatusCompleted, now); err != nil {
		return err
	}

	slog.Info("game settled", "room", roomCode, "winner", winnerID, "payout", payout, "house_fee", houseFee)
	return tx.Commit()
}

// RefundAllPlayers refunds all players minus house fee (e.g. disconnect/abort).
func (s *Service) RefundAllPlayers(roomCode string, playerIDs []string, stakePerPlayer float64) error {
	if stakePerPlayer <= 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	refund := stakePerPlayer * (1 - models.HouseFeePercent)
	now := time.Now()

	for _, pid := range playerIDs {
		var walletID int64
		if err := tx.QueryRow(`SELECT id FROM wallets WHERE player_id = ?`, pid).Scan(&walletID); err != nil {
			continue
		}

		if _, err := tx.Exec(`UPDATE wallets SET balance = balance + ?, updated_at = ? WHERE id = ?`, refund, now, walletID); err != nil {
			return err
		}

		if _, err := tx.Exec(`INSERT INTO transactions (wallet_id, type, amount, description, status, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			walletID, models.TxBetRefund, refund,
			fmt.Sprintf("Refund room %s: %.2f KES (minus 20%% fee)", roomCode, refund),
			models.TxStatusCompleted, now); err != nil {
			return err
		}
	}

	// Record house fee
	totalHouseFee := stakePerPlayer * models.HouseFeePercent * float64(len(playerIDs))
	if _, err := tx.Exec(`INSERT INTO transactions (wallet_id, type, amount, description, status, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		0, models.TxHouseFee, totalHouseFee,
		fmt.Sprintf("House fee refund room %s: %.2f KES", roomCode, totalHouseFee),
		models.TxStatusCompleted, now); err != nil {
		return err
	}

	return tx.Commit()
}

// Deposit adds funds to a player's wallet.
func (s *Service) Deposit(playerID string, amount float64, reference string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	w, err := s.getOrCreateWalletTx(tx, playerID)
	if err != nil {
		return err
	}

	now := time.Now()
	if _, err := tx.Exec(`UPDATE wallets SET balance = balance + ?, updated_at = ? WHERE id = ?`, amount, now, w.ID); err != nil {
		return err
	}

	if _, err := tx.Exec(`INSERT INTO transactions (wallet_id, type, amount, reference, description, status, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		w.ID, models.TxDeposit, amount, reference,
		fmt.Sprintf("Deposit: %.2f KES", amount),
		models.TxStatusCompleted, now); err != nil {
		return err
	}

	return tx.Commit()
}

// Withdraw deducts funds from a player's wallet (pending external payout).
func (s *Service) Withdraw(playerID string, amount float64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var walletID int64
	var balance float64
	if err := tx.QueryRow(`SELECT id, balance FROM wallets WHERE player_id = ?`, playerID).Scan(&walletID, &balance); err != nil {
		return fmt.Errorf("wallet not found: %w", err)
	}

	if balance < amount {
		return fmt.Errorf("insufficient balance")
	}

	now := time.Now()
	if _, err := tx.Exec(`UPDATE wallets SET balance = balance - ?, updated_at = ? WHERE id = ?`, amount, now, walletID); err != nil {
		return err
	}

	if _, err := tx.Exec(`INSERT INTO transactions (wallet_id, type, amount, description, status, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		walletID, models.TxWithdraw, -amount,
		fmt.Sprintf("Withdrawal: %.2f KES", amount),
		models.TxStatusPending, now); err != nil {
		return err
	}

	return tx.Commit()
}

// CreatePendingTransaction stores a pending deposit/withdraw for callback matching.
func (s *Service) CreatePendingTransaction(playerID string, txType models.TransactionType, amount float64, reference, description string) error {
	w, err := s.GetOrCreateWallet(playerID)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(`INSERT INTO transactions (wallet_id, type, amount, reference, description, status, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		w.ID, txType, amount, reference, description, models.TxStatusPending, time.Now())
	return err
}

// CompletePendingDeposit finds a pending deposit by reference, credits wallet, removes pending.
func (s *Service) CompletePendingDeposit(reference string, amount float64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var txnID int64
	var walletID int64
	if err := tx.QueryRow(`SELECT id, wallet_id FROM transactions WHERE reference = ? AND status = ?`,
		reference, models.TxStatusPending).Scan(&txnID, &walletID); err != nil {
		return fmt.Errorf("no pending transaction for ref %s: %w", reference, err)
	}

	var playerID string
	if err := tx.QueryRow(`SELECT player_id FROM wallets WHERE id = ?`, walletID).Scan(&playerID); err != nil {
		return err
	}

	now := time.Now()
	if _, err := tx.Exec(`UPDATE wallets SET balance = balance + ?, updated_at = ? WHERE id = ?`, amount, now, walletID); err != nil {
		return err
	}

	if _, err := tx.Exec(`INSERT INTO transactions (wallet_id, type, amount, reference, description, status, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		walletID, models.TxDeposit, amount, reference,
		fmt.Sprintf("Deposit completed: %.2f KES", amount),
		models.TxStatusCompleted, now); err != nil {
		return err
	}

	// Remove the pending record
	if _, err := tx.Exec(`DELETE FROM transactions WHERE id = ?`, txnID); err != nil {
		return err
	}

	slog.Info("deposit completed", "player", playerID, "amount", amount, "ref", reference)
	return tx.Commit()
}

// FailPendingTransaction marks a pending transaction as failed.
func (s *Service) FailPendingTransaction(reference string) error {
	_, err := s.db.Exec(`UPDATE transactions SET status = ? WHERE reference = ? AND status = ?`,
		models.TxStatusFailed, reference, models.TxStatusPending)
	return err
}

// RefundFailedWithdraw refunds a failed withdrawal by reference.
func (s *Service) RefundFailedWithdraw(reference string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var txnID, walletID int64
	var amount float64
	if err := tx.QueryRow(`SELECT id, wallet_id, amount FROM transactions WHERE reference = ? AND status = ? AND type = ?`,
		reference, models.TxStatusPending, models.TxWithdraw).Scan(&txnID, &walletID, &amount); err != nil {
		return err
	}

	refundAmt := -amount // amount is negative for withdrawals
	now := time.Now()

	if _, err := tx.Exec(`UPDATE wallets SET balance = balance + ?, updated_at = ? WHERE id = ?`, refundAmt, now, walletID); err != nil {
		return err
	}

	if _, err := tx.Exec(`UPDATE transactions SET status = ? WHERE id = ?`, models.TxStatusFailed, txnID); err != nil {
		return err
	}

	return tx.Commit()
}

// CompletePendingWithdraw marks a pending withdrawal as completed.
func (s *Service) CompletePendingWithdraw(reference string) error {
	_, err := s.db.Exec(`UPDATE transactions SET status = ? WHERE reference = ? AND status = ? AND type = ?`,
		models.TxStatusCompleted, reference, models.TxStatusPending, models.TxWithdraw)
	return err
}

// GetTransactions returns paginated transaction history.
func (s *Service) GetTransactions(playerID string, limit, offset int) ([]models.Transaction, error) {
	w, err := s.GetOrCreateWallet(playerID)
	if err != nil {
		return nil, err
	}

	rows, err := s.db.Query(`SELECT id, wallet_id, type, amount, COALESCE(reference,''), description, status, created_at
		FROM transactions WHERE wallet_id = ? ORDER BY created_at DESC LIMIT ? OFFSET ?`, w.ID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var txns []models.Transaction
	for rows.Next() {
		var t models.Transaction
		if err := rows.Scan(&t.ID, &t.WalletID, &t.Type, &t.Amount, &t.Reference, &t.Description, &t.Status, &t.CreatedAt); err != nil {
			return nil, err
		}
		txns = append(txns, t)
	}
	return txns, nil
}

func (s *Service) getOrCreateWalletTx(tx *sql.Tx, playerID string) (*models.Wallet, error) {
	var w models.Wallet
	err := tx.QueryRow(`SELECT id, player_id, balance, currency FROM wallets WHERE player_id = ?`, playerID).
		Scan(&w.ID, &w.PlayerID, &w.Balance, &w.Currency)
	if err == nil {
		return &w, nil
	}

	now := time.Now()
	res, err := tx.Exec(`INSERT INTO wallets (player_id, balance, currency, created_at, updated_at) VALUES (?, 0, 'KES', ?, ?)`,
		playerID, now, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &models.Wallet{ID: id, PlayerID: playerID, Balance: 0, Currency: "KES"}, nil
}
