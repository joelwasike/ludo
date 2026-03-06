package game

import "github.com/ludo/server/pkg/models"

// ComputeValidMoves calculates all valid moves for the current player
// given the dice value. Returns an empty slice if no moves are possible.
func ComputeValidMoves(state *models.GameState, diceValue int) []models.Move {
	player := GetCurrentPlayer(state)
	if player == nil {
		return nil
	}

	var moves []models.Move
	for i := range player.Tokens {
		token := &player.Tokens[i]
		if move, ok := computeMoveForToken(state, player, token, diceValue); ok {
			moves = append(moves, move)
		}
	}
	return moves
}

func computeMoveForToken(state *models.GameState, player *models.Player, token *models.Token, diceValue int) (models.Move, bool) {
	switch token.State {
	case models.InBase:
		return tryEnterBoard(state, player, token, diceValue)
	case models.OnBoard:
		return tryMoveOnBoard(state, player, token, diceValue)
	case models.InHome:
		return tryMoveInHome(state, player, token, diceValue)
	case models.Finished:
		return models.Move{}, false
	}
	return models.Move{}, false
}

// tryEnterBoard checks if a token in base can enter the board (requires a 6).
func tryEnterBoard(state *models.GameState, player *models.Player, token *models.Token, diceValue int) (models.Move, bool) {
	if diceValue != 6 {
		return models.Move{}, false
	}

	config := GetPlayerConfig(player.Color)
	startPos := config.StartPosition

	// Check if the start position is blocked by own token
	for _, t := range player.Tokens {
		if t.ID != token.ID && t.State == models.OnBoard && t.Position == startPos {
			return models.Move{}, false
		}
	}

	move := models.Move{
		TokenID:   token.ID,
		FromState: models.InBase,
		FromPos:   -1,
		ToState:   models.OnBoard,
		ToPos:     startPos,
	}

	// Check if an opponent's token is on the start position (capture)
	if capture := findCapturable(state, player.Color, startPos); capture != nil {
		move.IsCapture = true
		move.Captured = capture
	}

	return move, true
}

// tryMoveOnBoard computes a move for a token on the main track.
func tryMoveOnBoard(state *models.GameState, player *models.Player, token *models.Token, diceValue int) (models.Move, bool) {
	relativeSteps := RelativePosition(player.Color, token.Position)

	// Check if this move would enter the home column
	if CanEnterHome(relativeSteps, diceValue) {
		homePos := HomeColumnPosition(relativeSteps, diceValue)

		// Check for exact finish
		if IsExactFinish(homePos) {
			return models.Move{
				TokenID:   token.ID,
				FromState: models.OnBoard,
				FromPos:   token.Position,
				ToState:   models.Finished,
				ToPos:     homePos,
			}, true
		}

		// Check if valid home position (not overshoot)
		if IsValidHomePosition(homePos) {
			// Check if own token already occupies this home column position
			for _, t := range player.Tokens {
				if t.ID != token.ID && t.State == models.InHome && t.Position == homePos {
					return models.Move{}, false
				}
			}
			return models.Move{
				TokenID:   token.ID,
				FromState: models.OnBoard,
				FromPos:   token.Position,
				ToState:   models.InHome,
				ToPos:     homePos,
			}, true
		}

		// Overshoot — cannot move
		return models.Move{}, false
	}

	// Normal main track movement
	newGlobalPos := (token.Position + diceValue) % MainTrackSize

	// Check if own token is on the destination
	for _, t := range player.Tokens {
		if t.ID != token.ID && t.State == models.OnBoard && t.Position == newGlobalPos {
			return models.Move{}, false
		}
	}

	move := models.Move{
		TokenID:   token.ID,
		FromState: models.OnBoard,
		FromPos:   token.Position,
		ToState:   models.OnBoard,
		ToPos:     newGlobalPos,
	}

	// Check for capture
	if capture := findCapturable(state, player.Color, newGlobalPos); capture != nil {
		move.IsCapture = true
		move.Captured = capture
	}

	return move, true
}

// tryMoveInHome computes a move for a token in the home column.
func tryMoveInHome(state *models.GameState, player *models.Player, token *models.Token, diceValue int) (models.Move, bool) {
	newPos, finishes, valid := CanMoveInHomeColumn(token.Position, diceValue)
	if !valid {
		return models.Move{}, false
	}

	if finishes {
		return models.Move{
			TokenID:   token.ID,
			FromState: models.InHome,
			FromPos:   token.Position,
			ToState:   models.Finished,
			ToPos:     newPos,
		}, true
	}

	// Check if own token already at new home position
	for _, t := range player.Tokens {
		if t.ID != token.ID && t.State == models.InHome && t.Position == newPos {
			return models.Move{}, false
		}
	}

	return models.Move{
		TokenID:   token.ID,
		FromState: models.InHome,
		FromPos:   token.Position,
		ToState:   models.InHome,
		ToPos:     newPos,
	}, true
}

// findCapturable checks if an opponent's token is at the given main track
// position and can be captured. Returns nil if no capture possible.
func findCapturable(state *models.GameState, attackerColor models.PlayerColor, pos int) *models.CaptureInfo {
	// Cannot capture on safe zones
	if IsSafeZone(pos) {
		return nil
	}

	for i := range state.Players {
		p := &state.Players[i]
		if p.Color == attackerColor {
			continue
		}
		for _, t := range p.Tokens {
			if t.State == models.OnBoard && t.Position == pos {
				return &models.CaptureInfo{
					Color:   p.Color,
					TokenID: t.ID,
				}
			}
		}
	}
	return nil
}

// ApplyMove applies a validated move to the game state and returns events.
// This mutates the game state in place for efficiency.
func ApplyMove(state *models.GameState, move models.Move) []models.GameEvent {
	var events []models.GameEvent
	player := GetCurrentPlayer(state)

	// Move the token
	token := &player.Tokens[move.TokenID]
	token.State = move.ToState
	token.Position = move.ToPos

	// Handle capture
	if move.IsCapture && move.Captured != nil {
		capturedPlayer := GetPlayerByColor(state, move.Captured.Color)
		capturedToken := &capturedPlayer.Tokens[move.Captured.TokenID]
		capturedToken.State = models.InBase
		capturedToken.Position = -1

		events = append(events, models.GameEvent{
			Type: "token_captured",
			Payload: map[string]interface{}{
				"capturer":       player.Color,
				"captured_color": move.Captured.Color,
				"captured_token": move.Captured.TokenID,
			},
		})
	}

	// Check if token finished
	tokenFinished := move.ToState == models.Finished
	if tokenFinished {
		token.Position = -1 // Mark as done

		events = append(events, models.GameEvent{
			Type: "token_finished",
			Payload: map[string]interface{}{
				"player": player.Color,
				"token":  move.TokenID,
			},
		})

		// Check if player finished all tokens
		if HasAllTokensFinished(player) {
			state.FinishedCount++
			player.FinishOrder = state.FinishedCount

			events = append(events, models.GameEvent{
				Type: "player_finished",
				Payload: map[string]interface{}{
					"player":       player.Color,
					"finish_order": player.FinishOrder,
				},
			})

			// Check if first to finish (winner)
			if state.FinishedCount == 1 {
				color := player.Color
				state.Winner = &color
				events = append(events, models.GameEvent{
					Type: "game_winner",
					Payload: map[string]interface{}{
						"player": player.Color,
					},
				})
			}

			// Check if game is over
			if AllPlayersFinished(state) {
				events = append(events, models.GameEvent{
					Type:    "game_over",
					Payload: nil,
				})
				return events
			}
		}
	}

	// Determine next turn
	grantExtraTurn := false

	// Extra turn: rolled a 6 (and not three consecutive)
	if state.DiceValue == 6 && state.ConsecutiveSixes < 3 {
		grantExtraTurn = true
	}

	// Extra turn: captured an opponent
	if move.IsCapture {
		grantExtraTurn = true
	}

	// Extra turn: token finished
	if tokenFinished {
		grantExtraTurn = true
	}

	if grantExtraTurn && !HasAllTokensFinished(player) {
		// Same player goes again
		state.TurnPhase = models.WaitingForRoll
		// Keep consecutive sixes count if it was a 6
		// (it will be checked on next roll)
		events = append(events, models.GameEvent{
			Type: "extra_turn",
			Payload: map[string]interface{}{
				"player": player.Color,
				"reason": extraTurnReason(state.DiceValue == 6, move.IsCapture, tokenFinished),
			},
		})
	} else {
		// Advance to next player
		state.CurrentTurn = NextPlayerColor(state, state.CurrentTurn)
		state.TurnPhase = models.WaitingForRoll
		state.ConsecutiveSixes = 0
		state.TurnNumber++

		events = append(events, models.GameEvent{
			Type: "turn_changed",
			Payload: map[string]interface{}{
				"player": state.CurrentTurn,
			},
		})
	}

	state.DiceValue = 0
	state.ValidMoves = nil

	return events
}

// ApplyDiceRoll processes a dice roll and computes valid moves.
// Returns events and whether the player has any valid moves.
func ApplyDiceRoll(state *models.GameState, diceValue int) ([]models.GameEvent, bool) {
	var events []models.GameEvent

	state.DiceValue = diceValue

	// Track consecutive sixes
	if diceValue == 6 {
		state.ConsecutiveSixes++
	} else {
		state.ConsecutiveSixes = 0
	}

	// Three consecutive sixes: forfeit turn
	if state.ConsecutiveSixes >= 3 {
		state.ConsecutiveSixes = 0
		state.CurrentTurn = NextPlayerColor(state, state.CurrentTurn)
		state.TurnPhase = models.WaitingForRoll
		state.DiceValue = 0
		state.TurnNumber++

		events = append(events, models.GameEvent{
			Type: "three_sixes",
			Payload: map[string]interface{}{
				"player": state.CurrentTurn,
			},
		})
		events = append(events, models.GameEvent{
			Type: "turn_changed",
			Payload: map[string]interface{}{
				"player": state.CurrentTurn,
			},
		})
		return events, false
	}

	// Compute valid moves
	validMoves := ComputeValidMoves(state, diceValue)
	state.ValidMoves = validMoves

	if len(validMoves) == 0 {
		// No valid moves — skip turn
		state.CurrentTurn = NextPlayerColor(state, state.CurrentTurn)
		state.TurnPhase = models.WaitingForRoll
		state.DiceValue = 0
		state.ConsecutiveSixes = 0
		state.TurnNumber++

		events = append(events, models.GameEvent{
			Type: "no_valid_moves",
			Payload: map[string]interface{}{
				"player": state.CurrentTurn,
			},
		})
		events = append(events, models.GameEvent{
			Type: "turn_changed",
			Payload: map[string]interface{}{
				"player": state.CurrentTurn,
			},
		})
		return events, false
	}

	state.TurnPhase = models.WaitingForMove

	events = append(events, models.GameEvent{
		Type: "dice_rolled",
		Payload: map[string]interface{}{
			"player":      state.CurrentTurn,
			"value":       diceValue,
			"valid_moves": validMoves,
		},
	})

	return events, true
}

// FindMoveByTokenID finds a valid move for the given token ID.
func FindMoveByTokenID(state *models.GameState, tokenID int) (models.Move, bool) {
	for _, m := range state.ValidMoves {
		if m.TokenID == tokenID {
			return m, true
		}
	}
	return models.Move{}, false
}

func extraTurnReason(rolledSix, captured, finished bool) string {
	if finished {
		return "token_finished"
	}
	if captured {
		return "capture"
	}
	if rolledSix {
		return "rolled_six"
	}
	return "unknown"
}
