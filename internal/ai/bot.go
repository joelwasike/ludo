package ai

import "github.com/ludo/server/pkg/models"

// Strategy defines the interface for bot decision-making.
type Strategy interface {
	// ChooseMove selects a move from the list of valid moves.
	ChooseMove(state *models.GameState, validMoves []models.Move, botColor models.PlayerColor) models.Move
	// Difficulty returns the strategy name.
	Difficulty() string
}

// NewStrategy creates a strategy by name.
func NewStrategy(difficulty string) Strategy {
	switch difficulty {
	case "aggressive":
		return &AggressiveStrategy{}
	case "random":
		return &RandomStrategy{}
	default:
		return &BasicStrategy{}
	}
}
