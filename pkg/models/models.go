package models

import "time"

// PlayerColor represents the four player colors in Ludo.
type PlayerColor int

const (
	Red    PlayerColor = 0
	Green  PlayerColor = 1
	Yellow PlayerColor = 2
	Blue   PlayerColor = 3
)

func (c PlayerColor) String() string {
	switch c {
	case Red:
		return "Red"
	case Green:
		return "Green"
	case Yellow:
		return "Yellow"
	case Blue:
		return "Blue"
	default:
		return "Unknown"
	}
}

// TokenState represents where a token currently is.
type TokenState int

const (
	InBase   TokenState = 0
	OnBoard  TokenState = 1
	InHome   TokenState = 2
	Finished TokenState = 3
)

func (s TokenState) String() string {
	switch s {
	case InBase:
		return "InBase"
	case OnBoard:
		return "OnBoard"
	case InHome:
		return "InHome"
	case Finished:
		return "Finished"
	default:
		return "Unknown"
	}
}

// TurnPhase represents the current phase of a player's turn.
type TurnPhase int

const (
	WaitingForRoll TurnPhase = 0
	WaitingForMove TurnPhase = 1
)

// Token represents a single game token.
type Token struct {
	ID       int         `json:"id"`
	Color    PlayerColor `json:"color"`
	State    TokenState  `json:"state"`
	Position int         `json:"position"` // Main track: 0-51, Home: 0-5, Base/Finished: -1
}

// Player represents a player in the game.
type Player struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Color       PlayerColor `json:"color"`
	Tokens      [4]Token    `json:"tokens"`
	IsBot       bool        `json:"is_bot"`
	IsConnected bool        `json:"is_connected"`
	FinishOrder int         `json:"finish_order"` // 0=not finished
}

// Move represents a possible token move.
type Move struct {
	TokenID   int        `json:"token_id"`
	FromState TokenState `json:"from_state"`
	FromPos   int        `json:"from_pos"`
	ToState   TokenState `json:"to_state"`
	ToPos     int        `json:"to_pos"`
	IsCapture bool       `json:"is_capture"`
	Captured  *CaptureInfo `json:"captured,omitempty"`
}

// CaptureInfo describes a captured token.
type CaptureInfo struct {
	Color   PlayerColor `json:"color"`
	TokenID int         `json:"token_id"`
}

// GameState holds the entire state of a game.
type GameState struct {
	ID               string       `json:"id"`
	Players          []Player     `json:"players"`
	CurrentTurn      PlayerColor  `json:"current_turn"`
	TurnPhase        TurnPhase    `json:"turn_phase"`
	DiceValue        int          `json:"dice_value"`
	ConsecutiveSixes int          `json:"consecutive_sixes"`
	ValidMoves       []Move       `json:"valid_moves"`
	Winner           *PlayerColor `json:"winner,omitempty"`
	FinishedCount    int          `json:"finished_count"`
	TurnNumber       int          `json:"turn_number"`
	CreatedAt        time.Time    `json:"created_at"`
}

// GameEvent represents events that occur during gameplay.
type GameEvent struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}

// RoomStatus represents the state of a game room.
type RoomStatus int

const (
	RoomWaiting  RoomStatus = 0
	RoomPlaying  RoomStatus = 1
	RoomFinished RoomStatus = 2
)

// RoomSettings configurable per room.
type RoomSettings struct {
	MaxPlayers     int     `json:"max_players"`
	TurnTimeoutSec int     `json:"turn_timeout_sec"`
	AllowBots      bool    `json:"allow_bots"`
	IsPaid         bool    `json:"is_paid"`
	StakeAmount    float64 `json:"stake_amount"` // Per-player stake in KES
	Currency       string  `json:"currency"`
}

// RoomInfo is the public view of a room.
type RoomInfo struct {
	ID         string       `json:"id"`
	Code       string       `json:"code"`
	Status     RoomStatus   `json:"status"`
	Players    []PlayerInfo `json:"players"`
	MaxPlayers int          `json:"max_players"`
	HostID     string       `json:"host_id"`
	Settings   RoomSettings `json:"settings"`
	CreatedAt  time.Time    `json:"created_at"`
}

// PlayerInfo is the public view of a player in a room.
type PlayerInfo struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Color       PlayerColor `json:"color"`
	IsBot       bool        `json:"is_bot"`
	IsConnected bool        `json:"is_connected"`
	IsReady     bool        `json:"is_ready"`
}
