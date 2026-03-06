package room

import (
	"encoding/json"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/ludo/server/internal/ai"
	"github.com/ludo/server/internal/game"
	"github.com/ludo/server/internal/ws"
	"github.com/ludo/server/pkg/models"
)

// PlayerSession tracks a player's connection within a room.
type PlayerSession struct {
	PlayerID       string
	SessionToken   string
	DisplayName    string
	Color          models.PlayerColor
	IsBot          bool
	BotStrategy    ai.Strategy
	IsConnected    bool
	ReconnectUntil time.Time
}

// Room represents a single game room.
type Room struct {
	code       string
	hostID     string
	settings   models.RoomSettings
	status     models.RoomStatus
	players    []*PlayerSession
	gameState  *models.GameState
	hub        *ws.Hub
	messages   chan playerMessage
	done       chan struct{}
	turnTimer  *time.Timer
	mu         sync.RWMutex
	createdAt  time.Time
}

type playerMessage struct {
	playerID string
	msg      *ws.Message
}

// NewRoom creates a new room.
func NewRoom(code, hostID, hostName string, settings models.RoomSettings, hub *ws.Hub) *Room {
	if settings.MaxPlayers == 0 {
		settings.MaxPlayers = 4
	}
	if settings.TurnTimeoutSec == 0 {
		settings.TurnTimeoutSec = 30
	}

	r := &Room{
		code:      code,
		hostID:    hostID,
		settings:  settings,
		status:    models.RoomWaiting,
		hub:       hub,
		messages:  make(chan playerMessage, 64),
		done:      make(chan struct{}),
		createdAt: time.Now(),
	}

	// Add host as first player
	r.players = append(r.players, &PlayerSession{
		PlayerID:     hostID,
		SessionToken: uuid.New().String(),
		DisplayName:  hostName,
		Color:        models.Red,
		IsBot:        false,
		IsConnected:  true,
	})

	return r
}

// Run is the room's main loop, processing messages sequentially.
func (r *Room) Run() {
	defer func() {
		if r.turnTimer != nil {
			r.turnTimer.Stop()
		}
	}()

	for {
		select {
		case pm := <-r.messages:
			r.processMessage(pm.playerID, pm.msg)
		case <-r.done:
			return
		}
	}
}

// HandlePlayerMessage implements ws.RoomHandler.
func (r *Room) HandlePlayerMessage(playerID string, msg *ws.Message) {
	r.messages <- playerMessage{playerID: playerID, msg: msg}
}

// HandlePlayerDisconnect implements ws.RoomHandler.
func (r *Room) HandlePlayerDisconnect(playerID string) {
	r.mu.Lock()
	for _, p := range r.players {
		if p.PlayerID == playerID && !p.IsBot {
			p.IsConnected = false
			p.ReconnectUntil = time.Now().Add(120 * time.Second)
			slog.Info("player disconnected", "player", playerID, "room", r.code)
		}
	}
	r.mu.Unlock()

	r.broadcastRoomUpdate()
}

// HandlePlayerReconnect implements ws.RoomHandler.
func (r *Room) HandlePlayerReconnect(playerID string, client *ws.Client) {
	r.mu.Lock()
	for _, p := range r.players {
		if p.PlayerID == playerID {
			p.IsConnected = true
			slog.Info("player reconnected", "player", playerID, "room", r.code)
		}
	}
	r.mu.Unlock()

	// Send full game state sync
	if r.gameState != nil {
		data, _ := ws.NewMessage("game_state_sync", r.gameState)
		client.Send(data)
	}

	r.broadcastRoomUpdate()
}

// GetRoomInfo implements ws.RoomHandler.
func (r *Room) GetRoomInfo() *models.RoomInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var players []models.PlayerInfo
	for _, p := range r.players {
		players = append(players, models.PlayerInfo{
			ID:          p.PlayerID,
			Name:        p.DisplayName,
			Color:       p.Color,
			IsBot:       p.IsBot,
			IsConnected: p.IsConnected || p.IsBot,
		})
	}

	return &models.RoomInfo{
		ID:         r.code,
		Code:       r.code,
		Status:     r.status,
		Players:    players,
		MaxPlayers: r.settings.MaxPlayers,
		HostID:     r.hostID,
		Settings:   r.settings,
		CreatedAt:  r.createdAt,
	}
}

func (r *Room) processMessage(playerID string, msg *ws.Message) {
	switch msg.Type {
	case "join_room":
		r.handleJoin(playerID, msg)
	case "start_game":
		r.handleStartGame(playerID)
	case "roll_dice":
		r.handleRollDice(playerID)
	case "move_token":
		r.handleMoveToken(playerID, msg)
	case "add_bot":
		r.handleAddBot(playerID, msg)
	case "remove_bot":
		r.handleRemoveBot(playerID, msg)
	}
}

func (r *Room) handleJoin(playerID string, msg *ws.Message) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.status != models.RoomWaiting {
		errMsg, _ := ws.NewErrorMessage("GAME_IN_PROGRESS", "Game already started")
		r.hub.SendToPlayer(playerID, errMsg)
		return
	}

	if len(r.players) >= r.settings.MaxPlayers {
		errMsg, _ := ws.NewErrorMessage("ROOM_FULL", "Room is full")
		r.hub.SendToPlayer(playerID, errMsg)
		return
	}

	// Check if player already in room
	for _, p := range r.players {
		if p.PlayerID == playerID {
			return // Already joined
		}
	}

	var payload ws.JoinRoomPayload
	json.Unmarshal(msg.Payload, &payload)

	color := r.nextAvailableColor()
	session := &PlayerSession{
		PlayerID:     playerID,
		SessionToken: uuid.New().String(),
		DisplayName:  payload.PlayerName,
		Color:        color,
		IsBot:        false,
		IsConnected:  true,
	}
	r.players = append(r.players, session)

	// Send room_joined to the joining player
	data, _ := ws.NewMessage("room_joined", map[string]interface{}{
		"room":          r.GetRoomInfoUnsafe(),
		"session_token": session.SessionToken,
		"your_color":    color,
	})
	r.hub.SendToPlayer(playerID, data)

	// Broadcast room update to all
	r.broadcastRoomUpdateUnsafe()
}

func (r *Room) handleStartGame(playerID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if playerID != r.hostID {
		errMsg, _ := ws.NewErrorMessage("NOT_HOST", "Only the host can start the game")
		r.hub.SendToPlayer(playerID, errMsg)
		return
	}

	if len(r.players) < 2 {
		errMsg, _ := ws.NewErrorMessage("NOT_ENOUGH_PLAYERS", "Need at least 2 players")
		r.hub.SendToPlayer(playerID, errMsg)
		return
	}

	// Create game state
	var gamePlayers []models.Player
	for _, session := range r.players {
		gamePlayers = append(gamePlayers, game.NewPlayer(
			session.PlayerID,
			session.DisplayName,
			session.Color,
			session.IsBot,
		))
	}

	r.gameState = game.NewGameState(r.code, gamePlayers)
	r.status = models.RoomPlaying

	// Broadcast game started
	data, _ := ws.NewMessage("game_started", r.gameState)
	r.hub.BroadcastToRoom(r.code, data)

	// Start turn timer
	r.startTurnTimer()

	// If the first player is a bot, execute its turn
	r.tryBotTurn()
}

func (r *Room) handleRollDice(playerID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.gameState == nil || r.status != models.RoomPlaying {
		return
	}

	// Verify it's this player's turn
	currentPlayer := game.GetCurrentPlayer(r.gameState)
	if currentPlayer == nil || currentPlayer.ID != playerID {
		errMsg, _ := ws.NewErrorMessage("NOT_YOUR_TURN", "It's not your turn")
		r.hub.SendToPlayer(playerID, errMsg)
		return
	}

	if r.gameState.TurnPhase != models.WaitingForRoll {
		errMsg, _ := ws.NewErrorMessage("INVALID_ACTION", "Waiting for move, not roll")
		r.hub.SendToPlayer(playerID, errMsg)
		return
	}

	// Roll the dice
	diceValue, err := game.RollDice()
	if err != nil {
		slog.Error("dice roll failed", "error", err)
		return
	}

	events, hasValidMoves := game.ApplyDiceRoll(r.gameState, diceValue)

	// Broadcast dice rolled
	data, _ := ws.NewMessage("dice_rolled", map[string]interface{}{
		"player":      r.gameState.CurrentTurn,
		"value":       diceValue,
		"valid_moves": r.gameState.ValidMoves,
		"events":      events,
	})
	r.hub.BroadcastToRoom(r.code, data)

	if !hasValidMoves {
		// Turn was auto-advanced
		r.resetTurnTimer()
		r.tryBotTurn()
	} else {
		r.resetTurnTimer()
	}
}

func (r *Room) handleMoveToken(playerID string, msg *ws.Message) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.gameState == nil || r.status != models.RoomPlaying {
		return
	}

	currentPlayer := game.GetCurrentPlayer(r.gameState)
	if currentPlayer == nil || currentPlayer.ID != playerID {
		errMsg, _ := ws.NewErrorMessage("NOT_YOUR_TURN", "It's not your turn")
		r.hub.SendToPlayer(playerID, errMsg)
		return
	}

	if r.gameState.TurnPhase != models.WaitingForMove {
		errMsg, _ := ws.NewErrorMessage("INVALID_ACTION", "Roll the dice first")
		r.hub.SendToPlayer(playerID, errMsg)
		return
	}

	var payload ws.MoveTokenPayload
	json.Unmarshal(msg.Payload, &payload)

	// Validate move
	move, ok := game.FindMoveByTokenID(r.gameState, payload.TokenID)
	if !ok {
		errMsg, _ := ws.NewErrorMessage("INVALID_MOVE", "Invalid token move")
		r.hub.SendToPlayer(playerID, errMsg)
		return
	}

	// Apply move
	events := game.ApplyMove(r.gameState, move)

	// Broadcast token moved
	data, _ := ws.NewMessage("token_moved", map[string]interface{}{
		"player":   currentPlayer.Color,
		"token_id": move.TokenID,
		"move":     move,
		"events":   events,
	})
	r.hub.BroadcastToRoom(r.code, data)

	// Check game over
	if r.gameState.Winner != nil && game.AllPlayersFinished(r.gameState) {
		r.status = models.RoomFinished
		data, _ := ws.NewMessage("game_over", map[string]interface{}{
			"winner":  r.gameState.Winner,
			"players": r.gameState.Players,
		})
		r.hub.BroadcastToRoom(r.code, data)
		return
	}

	// Broadcast turn change
	data, _ = ws.NewMessage("turn_changed", map[string]interface{}{
		"current_turn": r.gameState.CurrentTurn,
		"turn_phase":   r.gameState.TurnPhase,
	})
	r.hub.BroadcastToRoom(r.code, data)

	r.resetTurnTimer()
	r.tryBotTurn()
}

func (r *Room) handleAddBot(playerID string, msg *ws.Message) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if playerID != r.hostID {
		errMsg, _ := ws.NewErrorMessage("NOT_HOST", "Only the host can add bots")
		r.hub.SendToPlayer(playerID, errMsg)
		return
	}

	if len(r.players) >= r.settings.MaxPlayers {
		errMsg, _ := ws.NewErrorMessage("ROOM_FULL", "Room is full")
		r.hub.SendToPlayer(playerID, errMsg)
		return
	}

	var payload ws.AddBotPayload
	json.Unmarshal(msg.Payload, &payload)

	color := r.nextAvailableColor()
	botID := "bot-" + uuid.New().String()[:8]
	strategy := ai.NewStrategy(payload.Difficulty)

	session := &PlayerSession{
		PlayerID:    botID,
		DisplayName: "Bot (" + strategy.Difficulty() + ")",
		Color:       color,
		IsBot:       true,
		BotStrategy: strategy,
		IsConnected: true,
	}
	r.players = append(r.players, session)

	r.broadcastRoomUpdateUnsafe()
}

func (r *Room) handleRemoveBot(playerID string, msg *ws.Message) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if playerID != r.hostID {
		return
	}

	var payload ws.RemoveBotPayload
	json.Unmarshal(msg.Payload, &payload)

	color := models.PlayerColor(payload.Color)
	for i, p := range r.players {
		if p.Color == color && p.IsBot {
			r.players = append(r.players[:i], r.players[i+1:]...)
			break
		}
	}

	r.broadcastRoomUpdateUnsafe()
}

func (r *Room) tryBotTurn() {
	if r.gameState == nil {
		return
	}

	currentPlayer := game.GetCurrentPlayer(r.gameState)
	if currentPlayer == nil || !currentPlayer.IsBot {
		return
	}

	// Find bot session
	var botSession *PlayerSession
	for _, p := range r.players {
		if p.PlayerID == currentPlayer.ID {
			botSession = p
			break
		}
	}
	if botSession == nil {
		return
	}

	// Execute bot turn in a goroutine with simulated delay
	go func() {
		// Simulate thinking time
		thinkTime := 800 + rand.Intn(1200)
		time.Sleep(time.Duration(thinkTime) * time.Millisecond)

		// Roll dice
		r.handleRollDice(botSession.PlayerID)

		// If waiting for move, choose and execute
		r.mu.RLock()
		phase := r.gameState.TurnPhase
		validMoves := r.gameState.ValidMoves
		r.mu.RUnlock()

		if phase == models.WaitingForMove && len(validMoves) > 0 {
			// Simulate looking at the board
			moveTime := 500 + rand.Intn(800)
			time.Sleep(time.Duration(moveTime) * time.Millisecond)

			r.mu.RLock()
			move := botSession.BotStrategy.ChooseMove(r.gameState, r.gameState.ValidMoves, botSession.Color)
			r.mu.RUnlock()

			moveMsg := &ws.Message{
				Type:    "move_token",
				Payload: mustMarshal(ws.MoveTokenPayload{TokenID: move.TokenID}),
			}
			r.messages <- playerMessage{playerID: botSession.PlayerID, msg: moveMsg}
		}
	}()
}

func (r *Room) startTurnTimer() {
	if r.turnTimer != nil {
		r.turnTimer.Stop()
	}
	r.turnTimer = time.AfterFunc(time.Duration(r.settings.TurnTimeoutSec)*time.Second, func() {
		r.handleTurnTimeout()
	})
}

func (r *Room) resetTurnTimer() {
	r.startTurnTimer()
}

func (r *Room) handleTurnTimeout() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.gameState == nil || r.status != models.RoomPlaying {
		return
	}

	currentPlayer := game.GetCurrentPlayer(r.gameState)
	if currentPlayer == nil || currentPlayer.IsBot {
		return
	}

	slog.Info("turn timeout", "player", currentPlayer.ID, "room", r.code)

	// Auto-roll if waiting for roll
	if r.gameState.TurnPhase == models.WaitingForRoll {
		diceValue, err := game.RollDice()
		if err != nil {
			return
		}
		game.ApplyDiceRoll(r.gameState, diceValue)

		data, _ := ws.NewMessage("dice_rolled", map[string]interface{}{
			"player":      r.gameState.CurrentTurn,
			"value":       diceValue,
			"valid_moves": r.gameState.ValidMoves,
			"auto":        true,
		})
		r.hub.BroadcastToRoom(r.code, data)
	}

	// Auto-move if waiting for move
	if r.gameState.TurnPhase == models.WaitingForMove && len(r.gameState.ValidMoves) > 0 {
		// Pick the first valid move (basic auto-move)
		move := r.gameState.ValidMoves[0]
		events := game.ApplyMove(r.gameState, move)

		data, _ := ws.NewMessage("token_moved", map[string]interface{}{
			"player":   currentPlayer.Color,
			"token_id": move.TokenID,
			"move":     move,
			"events":   events,
			"auto":     true,
		})
		r.hub.BroadcastToRoom(r.code, data)
	}

	r.resetTurnTimer()
	r.tryBotTurn()
}

func (r *Room) nextAvailableColor() models.PlayerColor {
	used := make(map[models.PlayerColor]bool)
	for _, p := range r.players {
		used[p.Color] = true
	}
	colors := []models.PlayerColor{models.Red, models.Green, models.Yellow, models.Blue}
	for _, c := range colors {
		if !used[c] {
			return c
		}
	}
	return models.Red // Shouldn't happen
}

func (r *Room) broadcastRoomUpdate() {
	r.mu.RLock()
	defer r.mu.RUnlock()
	r.broadcastRoomUpdateUnsafe()
}

func (r *Room) broadcastRoomUpdateUnsafe() {
	info := r.GetRoomInfoUnsafe()
	data, _ := ws.NewMessage("room_updated", info)
	r.hub.BroadcastToRoom(r.code, data)
}

// GetRoomInfoUnsafe returns room info without locking (caller must hold lock).
func (r *Room) GetRoomInfoUnsafe() *models.RoomInfo {
	var players []models.PlayerInfo
	for _, p := range r.players {
		players = append(players, models.PlayerInfo{
			ID:          p.PlayerID,
			Name:        p.DisplayName,
			Color:       p.Color,
			IsBot:       p.IsBot,
			IsConnected: p.IsConnected || p.IsBot,
		})
	}

	return &models.RoomInfo{
		ID:         r.code,
		Code:       r.code,
		Status:     r.status,
		Players:    players,
		MaxPlayers: r.settings.MaxPlayers,
		HostID:     r.hostID,
		Settings:   r.settings,
		CreatedAt:  r.createdAt,
	}
}

func mustMarshal(v interface{}) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}
