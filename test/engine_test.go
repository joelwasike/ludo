package test

import (
	"testing"

	"github.com/ludo/server/internal/ai"
	"github.com/ludo/server/internal/game"
	"github.com/ludo/server/pkg/models"
)

func createTestGame() *models.GameState {
	players := []models.Player{
		game.NewPlayer("p1", "Red Player", models.Red, false),
		game.NewPlayer("p2", "Green Player", models.Green, false),
		game.NewPlayer("p3", "Yellow Player", models.Yellow, false),
		game.NewPlayer("p4", "Blue Player", models.Blue, false),
	}
	return game.NewGameState("test-game", players)
}

func TestNewGameState(t *testing.T) {
	state := createTestGame()

	if len(state.Players) != 4 {
		t.Errorf("expected 4 players, got %d", len(state.Players))
	}

	if state.CurrentTurn != models.Red {
		t.Errorf("expected Red to go first, got %v", state.CurrentTurn)
	}

	if state.TurnPhase != models.WaitingForRoll {
		t.Errorf("expected WaitingForRoll, got %v", state.TurnPhase)
	}

	// All tokens should be in base
	for _, p := range state.Players {
		for _, token := range p.Tokens {
			if token.State != models.InBase {
				t.Errorf("expected token %d to be InBase, got %v", token.ID, token.State)
			}
		}
	}
}

func TestNoMovesWithoutSix(t *testing.T) {
	state := createTestGame()
	moves := game.ComputeValidMoves(state, 3)

	if len(moves) != 0 {
		t.Errorf("expected no valid moves without rolling 6, got %d", len(moves))
	}
}

func TestEnterBoardOnSix(t *testing.T) {
	state := createTestGame()
	moves := game.ComputeValidMoves(state, 6)

	if len(moves) == 0 {
		t.Fatal("expected valid moves when rolling 6 with tokens in base")
	}

	// All 4 tokens should be able to enter
	if len(moves) != 4 {
		t.Errorf("expected 4 valid moves (all tokens can enter), got %d", len(moves))
	}

	for _, m := range moves {
		if m.FromState != models.InBase {
			t.Errorf("expected move from InBase, got %v", m.FromState)
		}
		if m.ToState != models.OnBoard {
			t.Errorf("expected move to OnBoard, got %v", m.ToState)
		}
		if m.ToPos != 0 { // Red starts at position 0
			t.Errorf("expected target position 0 for Red, got %d", m.ToPos)
		}
	}
}

func TestTokenMovementOnBoard(t *testing.T) {
	state := createTestGame()

	// Manually place Red token 0 on board at position 5
	state.Players[0].Tokens[0].State = models.OnBoard
	state.Players[0].Tokens[0].Position = 5

	moves := game.ComputeValidMoves(state, 4)

	found := false
	for _, m := range moves {
		if m.TokenID == 0 && m.FromPos == 5 && m.ToPos == 9 {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected token 0 to move from 5 to 9 with dice 4")
	}
}

func TestCaptureOpponent(t *testing.T) {
	state := createTestGame()

	// Place Red token 0 at position 5
	state.Players[0].Tokens[0].State = models.OnBoard
	state.Players[0].Tokens[0].Position = 5

	// Place Green token 0 at position 9 (not a safe zone)
	state.Players[1].Tokens[0].State = models.OnBoard
	state.Players[1].Tokens[0].Position = 9

	moves := game.ComputeValidMoves(state, 4)

	found := false
	for _, m := range moves {
		if m.TokenID == 0 && m.ToPos == 9 && m.IsCapture {
			found = true
			if m.Captured == nil {
				t.Error("capture info should not be nil")
			} else if m.Captured.Color != models.Green {
				t.Errorf("expected to capture Green, got %v", m.Captured.Color)
			}
			break
		}
	}
	if !found {
		t.Error("expected capture move for Red token 0")
	}
}

func TestNoCaptureOnSafeZone(t *testing.T) {
	state := createTestGame()

	// Place Red token 0 at position 6
	state.Players[0].Tokens[0].State = models.OnBoard
	state.Players[0].Tokens[0].Position = 6

	// Place Green token 0 at position 8 (safe zone)
	state.Players[1].Tokens[0].State = models.OnBoard
	state.Players[1].Tokens[0].Position = 8

	moves := game.ComputeValidMoves(state, 2)

	for _, m := range moves {
		if m.TokenID == 0 && m.ToPos == 8 && m.IsCapture {
			t.Error("should not capture on safe zone")
		}
	}
}

func TestHomeColumnEntry(t *testing.T) {
	state := createTestGame()

	// Red's home entry is at relative position 50 (global 50)
	// Place Red token at global position 49 (relative step 49)
	state.Players[0].Tokens[0].State = models.OnBoard
	state.Players[0].Tokens[0].Position = 49

	// Rolling 3 should put the token at home column position 0
	// relative 49 + 3 = 52, home pos = 52 - 51 - 1 = 0
	moves := game.ComputeValidMoves(state, 3)

	found := false
	for _, m := range moves {
		if m.TokenID == 0 && m.ToState == models.InHome && m.ToPos == 0 {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected Red token to enter home column at position 0")
	}
}

func TestExactFinish(t *testing.T) {
	state := createTestGame()

	// Place Red token in home column at position 3
	state.Players[0].Tokens[0].State = models.InHome
	state.Players[0].Tokens[0].Position = 3

	// Rolling 2 should finish the token (3+2=5 = HomeColumnSize-1)
	moves := game.ComputeValidMoves(state, 2)

	found := false
	for _, m := range moves {
		if m.TokenID == 0 && m.ToState == models.Finished {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected token to finish with exact roll")
	}
}

func TestOvershootInHome(t *testing.T) {
	state := createTestGame()

	// Place Red token in home column at position 4
	state.Players[0].Tokens[0].State = models.InHome
	state.Players[0].Tokens[0].Position = 4

	// Rolling 3 would overshoot (4+3=7 > 5)
	moves := game.ComputeValidMoves(state, 3)

	for _, m := range moves {
		if m.TokenID == 0 {
			t.Error("should not have valid move for token 0 (overshoot)")
		}
	}
}

func TestApplyMoveCapture(t *testing.T) {
	state := createTestGame()

	state.Players[0].Tokens[0].State = models.OnBoard
	state.Players[0].Tokens[0].Position = 5

	state.Players[1].Tokens[0].State = models.OnBoard
	state.Players[1].Tokens[0].Position = 9

	state.DiceValue = 4
	move := models.Move{
		TokenID:   0,
		FromState: models.OnBoard,
		FromPos:   5,
		ToState:   models.OnBoard,
		ToPos:     9,
		IsCapture: true,
		Captured:  &models.CaptureInfo{Color: models.Green, TokenID: 0},
	}

	events := game.ApplyMove(state, move)

	// Red token should be at position 9
	if state.Players[0].Tokens[0].Position != 9 {
		t.Errorf("expected Red token at 9, got %d", state.Players[0].Tokens[0].Position)
	}

	// Green token should be back in base
	if state.Players[1].Tokens[0].State != models.InBase {
		t.Errorf("expected Green token InBase, got %v", state.Players[1].Tokens[0].State)
	}

	// Should have capture event
	hasCaptureEvent := false
	for _, e := range events {
		if e.Type == "token_captured" {
			hasCaptureEvent = true
		}
	}
	if !hasCaptureEvent {
		t.Error("expected token_captured event")
	}
}

func TestThreeConsecutiveSixes(t *testing.T) {
	state := createTestGame()
	state.ConsecutiveSixes = 2

	events, hasValidMoves := game.ApplyDiceRoll(state, 6)

	if hasValidMoves {
		t.Error("should not have valid moves after three sixes")
	}

	hasThreeSixes := false
	for _, e := range events {
		if e.Type == "three_sixes" {
			hasThreeSixes = true
		}
	}
	if !hasThreeSixes {
		t.Error("expected three_sixes event")
	}
}

func TestBotSimulation(t *testing.T) {
	// Run a full game with 4 bots to verify the engine works end-to-end
	players := []models.Player{
		game.NewPlayer("bot1", "Bot Red", models.Red, true),
		game.NewPlayer("bot2", "Bot Green", models.Green, true),
		game.NewPlayer("bot3", "Bot Yellow", models.Yellow, true),
		game.NewPlayer("bot4", "Bot Blue", models.Blue, true),
	}

	state := game.NewGameState("sim-game", players)
	strategy := ai.NewStrategy("basic")

	maxTurns := 5000
	turns := 0

	for turns < maxTurns {
		// Roll dice
		diceValue, err := game.RollDice()
		if err != nil {
			t.Fatalf("dice roll error: %v", err)
		}

		_, hasValidMoves := game.ApplyDiceRoll(state, diceValue)

		if hasValidMoves && len(state.ValidMoves) > 0 {
			// Bot chooses a move
			move := strategy.ChooseMove(state, state.ValidMoves, state.CurrentTurn)
			game.ApplyMove(state, move)
		}

		turns++

		if game.AllPlayersFinished(state) {
			break
		}
	}

	if state.Winner == nil {
		t.Logf("game did not produce a winner in %d turns (may happen occasionally)", maxTurns)
	} else {
		t.Logf("game ended after %d turns, winner: %v", turns, state.Winner)
	}

	if state.FinishedCount == 0 && turns >= maxTurns {
		t.Error("no player finished in 5000 turns — likely a bug")
	}
}

func TestGlobalPosition(t *testing.T) {
	tests := []struct {
		color    models.PlayerColor
		relative int
		expected int
	}{
		{models.Red, 0, 0},
		{models.Red, 13, 13},
		{models.Green, 0, 13},
		{models.Green, 5, 18},
		{models.Yellow, 0, 26},
		{models.Blue, 0, 39},
		{models.Red, 51, 51},
		{models.Green, 51, 12},
	}

	for _, tc := range tests {
		got := game.GlobalPosition(tc.color, tc.relative)
		if got != tc.expected {
			t.Errorf("GlobalPosition(%v, %d) = %d, want %d", tc.color, tc.relative, got, tc.expected)
		}
	}
}

func TestRelativePosition(t *testing.T) {
	tests := []struct {
		color    models.PlayerColor
		global   int
		expected int
	}{
		{models.Red, 0, 0},
		{models.Red, 13, 13},
		{models.Green, 13, 0},
		{models.Green, 18, 5},
		{models.Yellow, 26, 0},
		{models.Blue, 39, 0},
		{models.Green, 12, 51},
	}

	for _, tc := range tests {
		got := game.RelativePosition(tc.color, tc.global)
		if got != tc.expected {
			t.Errorf("RelativePosition(%v, %d) = %d, want %d", tc.color, tc.global, got, tc.expected)
		}
	}
}

func TestBlockedByOwnToken(t *testing.T) {
	state := createTestGame()

	// Place Red token 0 on board at start position (0)
	state.Players[0].Tokens[0].State = models.OnBoard
	state.Players[0].Tokens[0].Position = 0

	// Rolling 6: token 0 can't enter (already at start), but tokens 1-3 can't either
	// because token 0 is already at start position
	moves := game.ComputeValidMoves(state, 6)

	// Token 0 should be able to move forward 6 steps (to position 6)
	// Tokens 1-3 cannot enter because position 0 is blocked
	for _, m := range moves {
		if m.FromState == models.InBase && m.ToPos == 0 {
			t.Error("should not be able to enter when own token blocks start")
		}
	}

	// Token 0 should have a valid move (from 0 to 6)
	found := false
	for _, m := range moves {
		if m.TokenID == 0 && m.FromState == models.OnBoard && m.ToPos == 6 {
			found = true
		}
	}
	if !found {
		t.Error("token 0 should be able to move from 0 to 6")
	}
}
