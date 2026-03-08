package storage

import (
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

// DB wraps the SQLite database connection.
type DB struct {
	conn *sql.DB
}

// New opens or creates the SQLite database.
func New(dbPath string) (*DB, error) {
	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	conn, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}

	// Test connection
	if err := conn.Ping(); err != nil {
		return nil, err
	}

	db := &DB{conn: conn}
	if err := db.migrate(); err != nil {
		return nil, err
	}

	slog.Info("database initialized", "path", dbPath)
	return db, nil
}

// Close closes the database connection.
func (db *DB) Close() error {
	return db.conn.Close()
}

// Conn returns the underlying sql.DB connection.
func (db *DB) Conn() *sql.DB {
	return db.conn
}

func (db *DB) migrate() error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS players (
			id TEXT PRIMARY KEY,
			display_name TEXT NOT NULL,
			games_played INTEGER DEFAULT 0,
			games_won INTEGER DEFAULT 0,
			tokens_finished INTEGER DEFAULT 0,
			tokens_captured INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_played_at DATETIME
		)`,
		`CREATE TABLE IF NOT EXISTS game_history (
			id TEXT PRIMARY KEY,
			started_at DATETIME NOT NULL,
			ended_at DATETIME,
			player_count INTEGER NOT NULL,
			winner_id TEXT,
			turns_total INTEGER,
			FOREIGN KEY (winner_id) REFERENCES players(id)
		)`,
		`CREATE TABLE IF NOT EXISTS game_players (
			game_id TEXT NOT NULL,
			player_id TEXT NOT NULL,
			color INTEGER NOT NULL,
			finish_order INTEGER DEFAULT 0,
			tokens_captured INTEGER DEFAULT 0,
			is_bot BOOLEAN DEFAULT FALSE,
			PRIMARY KEY (game_id, player_id),
			FOREIGN KEY (game_id) REFERENCES game_history(id),
			FOREIGN KEY (player_id) REFERENCES players(id)
		)`,
		`CREATE TABLE IF NOT EXISTS wallets (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			player_id TEXT NOT NULL UNIQUE,
			balance REAL DEFAULT 0,
			currency TEXT DEFAULT 'KES',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS transactions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			wallet_id INTEGER NOT NULL,
			type TEXT NOT NULL,
			amount REAL NOT NULL,
			reference TEXT DEFAULT '',
			description TEXT DEFAULT '',
			status TEXT DEFAULT 'pending',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (wallet_id) REFERENCES wallets(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_transactions_wallet ON transactions(wallet_id)`,
		`CREATE INDEX IF NOT EXISTS idx_transactions_reference ON transactions(reference)`,
	}

	for _, m := range migrations {
		if _, err := db.conn.Exec(m); err != nil {
			return err
		}
	}
	return nil
}

// UpsertPlayer creates or updates a player record.
func (db *DB) UpsertPlayer(id, name string) error {
	_, err := db.conn.Exec(`
		INSERT INTO players (id, display_name) VALUES (?, ?)
		ON CONFLICT(id) DO UPDATE SET display_name = excluded.display_name, last_played_at = CURRENT_TIMESTAMP
	`, id, name)
	return err
}

// IncrementPlayerStats updates player stats after a game.
func (db *DB) IncrementPlayerStats(playerID string, won bool, tokensFinished, tokensCaptured int) error {
	wonInt := 0
	if won {
		wonInt = 1
	}
	_, err := db.conn.Exec(`
		UPDATE players SET
			games_played = games_played + 1,
			games_won = games_won + ?,
			tokens_finished = tokens_finished + ?,
			tokens_captured = tokens_captured + ?,
			last_played_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, wonInt, tokensFinished, tokensCaptured, playerID)
	return err
}

// PlayerStats holds a player's statistics.
type PlayerStats struct {
	ID             string `json:"id"`
	DisplayName    string `json:"display_name"`
	GamesPlayed    int    `json:"games_played"`
	GamesWon       int    `json:"games_won"`
	TokensFinished int    `json:"tokens_finished"`
	TokensCaptured int    `json:"tokens_captured"`
}

// GetPlayerStats retrieves a player's stats.
func (db *DB) GetPlayerStats(playerID string) (*PlayerStats, error) {
	var stats PlayerStats
	err := db.conn.QueryRow(`
		SELECT id, display_name, games_played, games_won, tokens_finished, tokens_captured
		FROM players WHERE id = ?
	`, playerID).Scan(&stats.ID, &stats.DisplayName, &stats.GamesPlayed, &stats.GamesWon, &stats.TokensFinished, &stats.TokensCaptured)
	if err != nil {
		return nil, err
	}
	return &stats, nil
}

// SaveGameHistory saves a completed game to history.
func (db *DB) SaveGameHistory(gameID string, playerCount, totalTurns int, winnerID string) error {
	_, err := db.conn.Exec(`
		INSERT INTO game_history (id, started_at, ended_at, player_count, winner_id, turns_total)
		VALUES (?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, ?, ?, ?)
	`, gameID, playerCount, winnerID, totalTurns)
	return err
}
