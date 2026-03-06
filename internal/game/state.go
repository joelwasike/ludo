package game

import (
	"time"

	"github.com/ludo/server/pkg/models"
)

// NewToken creates a token in the base.
func NewToken(id int, color models.PlayerColor) models.Token {
	return models.Token{
		ID:       id,
		Color:    color,
		State:    models.InBase,
		Position: -1,
	}
}

// NewPlayer creates a player with 4 tokens in base.
func NewPlayer(id, name string, color models.PlayerColor, isBot bool) models.Player {
	p := models.Player{
		ID:          id,
		Name:        name,
		Color:       color,
		IsBot:       isBot,
		IsConnected: !isBot, // bots are always "connected"
		FinishOrder: 0,
	}
	for i := 0; i < TokensPerPlayer; i++ {
		p.Tokens[i] = NewToken(i, color)
	}
	return p
}

// NewGameState creates a fresh game state for the given players.
func NewGameState(id string, players []models.Player) *models.GameState {
	return &models.GameState{
		ID:               id,
		Players:          players,
		CurrentTurn:      players[0].Color,
		TurnPhase:        models.WaitingForRoll,
		DiceValue:        0,
		ConsecutiveSixes: 0,
		ValidMoves:       nil,
		Winner:           nil,
		FinishedCount:    0,
		TurnNumber:       1,
		CreatedAt:        time.Now(),
	}
}

// GetCurrentPlayer returns a pointer to the current turn's player.
func GetCurrentPlayer(state *models.GameState) *models.Player {
	for i := range state.Players {
		if state.Players[i].Color == state.CurrentTurn {
			return &state.Players[i]
		}
	}
	return nil
}

// GetPlayerByColor returns a pointer to the player with the given color.
func GetPlayerByColor(state *models.GameState, color models.PlayerColor) *models.Player {
	for i := range state.Players {
		if state.Players[i].Color == color {
			return &state.Players[i]
		}
	}
	return nil
}

// CountFinishedTokens counts how many of a player's tokens have finished.
func CountFinishedTokens(player *models.Player) int {
	count := 0
	for _, t := range player.Tokens {
		if t.State == models.Finished {
			count++
		}
	}
	return count
}

// HasAllTokensFinished checks if all 4 tokens of a player have finished.
func HasAllTokensFinished(player *models.Player) bool {
	return CountFinishedTokens(player) == TokensPerPlayer
}

// NextPlayerColor returns the next player's color, skipping players
// who have already finished all their tokens.
func NextPlayerColor(state *models.GameState, current models.PlayerColor) models.PlayerColor {
	numPlayers := len(state.Players)
	// Find current player index
	currentIdx := -1
	for i, p := range state.Players {
		if p.Color == current {
			currentIdx = i
			break
		}
	}

	// Try each next player in order
	for offset := 1; offset <= numPlayers; offset++ {
		nextIdx := (currentIdx + offset) % numPlayers
		nextPlayer := &state.Players[nextIdx]
		if !HasAllTokensFinished(nextPlayer) {
			return nextPlayer.Color
		}
	}
	// All players finished — should not happen in normal flow
	return current
}

// AllPlayersFinished checks if the game is over (all players finished
// or only one player remains unfinished).
func AllPlayersFinished(state *models.GameState) bool {
	unfinished := 0
	for _, p := range state.Players {
		if !HasAllTokensFinished(&p) {
			unfinished++
		}
	}
	return unfinished <= 1
}
