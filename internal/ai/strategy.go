package ai

import (
	"math/rand"
	"sort"

	"github.com/ludo/server/pkg/models"
	"github.com/ludo/server/internal/game"
)

// RandomStrategy picks a random valid move.
type RandomStrategy struct{}

func (s *RandomStrategy) ChooseMove(_ *models.GameState, validMoves []models.Move, _ models.PlayerColor) models.Move {
	return validMoves[rand.Intn(len(validMoves))]
}

func (s *RandomStrategy) Difficulty() string { return "random" }

// BasicStrategy uses a priority-based heuristic.
type BasicStrategy struct{}

func (s *BasicStrategy) ChooseMove(state *models.GameState, validMoves []models.Move, botColor models.PlayerColor) models.Move {
	return pickBestMove(validMoves, botColor, false)
}

func (s *BasicStrategy) Difficulty() string { return "basic" }

// AggressiveStrategy prioritizes capturing opponents.
type AggressiveStrategy struct{}

func (s *AggressiveStrategy) ChooseMove(state *models.GameState, validMoves []models.Move, botColor models.PlayerColor) models.Move {
	return pickBestMove(validMoves, botColor, true)
}

func (s *AggressiveStrategy) Difficulty() string { return "aggressive" }

type scoredMove struct {
	move  models.Move
	score int
}

func pickBestMove(validMoves []models.Move, botColor models.PlayerColor, aggressive bool) models.Move {
	scored := make([]scoredMove, len(validMoves))

	for i, m := range validMoves {
		score := 0

		// Finishing a token is always great
		if m.ToState == models.Finished {
			if aggressive {
				score += 800
			} else {
				score += 1000
			}
		}

		// Capturing is very valuable
		if m.IsCapture {
			if aggressive {
				score += 1000 // Aggressive: capture is highest priority
			} else {
				score += 500
			}
		}

		// Entering the board from base
		if m.FromState == models.InBase && m.ToState == models.OnBoard {
			score += 200
		}

		// Landing on a safe zone
		if m.ToState == models.OnBoard && game.IsSafeZone(m.ToPos) {
			score += 100
		}

		// Moving further in home column
		if m.ToState == models.InHome {
			score += 50 + m.ToPos*10 // Further = better
		}

		// Advancing on main track: prefer tokens closer to home
		if m.FromState == models.OnBoard && m.ToState == models.OnBoard {
			relSteps := game.RelativePosition(botColor, m.ToPos)
			score += relSteps // More advanced tokens get higher score
		}

		// For aggressive: bonus for moving toward opponents
		if aggressive && m.ToState == models.OnBoard {
			// Not a full opponent-proximity calculation, just a bonus
			// for being on the board and advancing
			score += 20
		}

		scored[i] = scoredMove{m, score}
	}

	// Sort by score descending
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	// Collect all moves with the top score (random tiebreak)
	topScore := scored[0].score
	var topMoves []models.Move
	for _, sm := range scored {
		if sm.score == topScore {
			topMoves = append(topMoves, sm.move)
		} else {
			break
		}
	}

	return topMoves[rand.Intn(len(topMoves))]
}
