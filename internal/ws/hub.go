package ws

import (
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/ludo/server/pkg/models"
)

// RoomHandler is the interface the hub uses to route messages to rooms.
type RoomHandler interface {
	HandlePlayerMessage(playerID string, msg *Message)
	HandlePlayerDisconnect(playerID string)
	HandlePlayerReconnect(playerID string, client *Client)
	GetRoomInfo() *models.RoomInfo
}

// Hub manages all WebSocket clients and routes messages.
type Hub struct {
	clients    map[string]*Client // sessionID -> Client
	rooms      map[string]RoomHandler
	register   chan *Client
	unregister chan *Client
	mu         sync.RWMutex
}

// NewHub creates a new Hub.
func NewHub() *Hub {
	return &Hub{
		clients:    make(map[string]*Client),
		rooms:      make(map[string]RoomHandler),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

// Run starts the hub's main loop for handling register/unregister events.
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client.SessionID] = client
			h.mu.Unlock()
			slog.Info("client registered", "player", client.PlayerID, "session", client.SessionID)

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client.SessionID]; ok {
				delete(h.clients, client.SessionID)
				slog.Info("client unregistered", "player", client.PlayerID, "session", client.SessionID)

				// Notify room of disconnect
				if client.RoomID != "" {
					if room, ok := h.rooms[client.RoomID]; ok {
						room.HandlePlayerDisconnect(client.PlayerID)
					}
				}
			}
			h.mu.Unlock()
		}
	}
}

// RegisterClient registers a new client with the hub.
func (h *Hub) RegisterClient(client *Client) {
	h.register <- client
}

// RegisterRoom registers a room handler.
func (h *Hub) RegisterRoom(roomID string, handler RoomHandler) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.rooms[roomID] = handler
}

// UnregisterRoom removes a room handler.
func (h *Hub) UnregisterRoom(roomID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.rooms, roomID)
}

// GetRoom returns a room handler by ID.
func (h *Hub) GetRoom(roomID string) (RoomHandler, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	room, ok := h.rooms[roomID]
	return room, ok
}

// GetClient returns a client by session ID.
func (h *Hub) GetClient(sessionID string) (*Client, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	client, ok := h.clients[sessionID]
	return client, ok
}

// HandleMessage routes a message from a client to the appropriate handler.
func (h *Hub) HandleMessage(client *Client, msg *Message) {
	switch msg.Type {
	case "ping":
		data, _ := NewMessage("pong", map[string]int64{
			"server_time": unixMillis(),
		})
		client.Send(data)

	case "join_room":
		var payload JoinRoomPayload
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			errMsg, _ := NewErrorMessage("INVALID_PAYLOAD", "Invalid join_room payload")
			client.Send(errMsg)
			return
		}
		h.handleJoinRoom(client, payload)

	case "leave_room":
		h.handleLeaveRoom(client)

	case "reconnect":
		var payload ReconnectPayload
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			errMsg, _ := NewErrorMessage("INVALID_PAYLOAD", "Invalid reconnect payload")
			client.Send(errMsg)
			return
		}
		h.handleReconnect(client, payload)

	default:
		// Route to room handler
		if client.RoomID == "" {
			errMsg, _ := NewErrorMessage("NOT_IN_ROOM", "You must join a room first")
			client.Send(errMsg)
			return
		}
		h.mu.RLock()
		room, ok := h.rooms[client.RoomID]
		h.mu.RUnlock()
		if !ok {
			errMsg, _ := NewErrorMessage("ROOM_NOT_FOUND", "Room no longer exists")
			client.Send(errMsg)
			return
		}
		room.HandlePlayerMessage(client.PlayerID, msg)
	}
}

func (h *Hub) handleJoinRoom(client *Client, payload JoinRoomPayload) {
	h.mu.RLock()
	room, ok := h.rooms[payload.Code]
	h.mu.RUnlock()

	if !ok {
		errMsg, _ := NewErrorMessage("ROOM_NOT_FOUND", "Room not found")
		client.Send(errMsg)
		return
	}

	client.RoomID = payload.Code
	// The room manager will handle the actual join logic
	room.HandlePlayerMessage(client.PlayerID, &Message{
		Type:    "join_room",
		Payload: mustMarshal(payload),
	})
}

func (h *Hub) handleLeaveRoom(client *Client) {
	if client.RoomID == "" {
		return
	}
	h.mu.RLock()
	room, ok := h.rooms[client.RoomID]
	h.mu.RUnlock()
	if ok {
		room.HandlePlayerDisconnect(client.PlayerID)
	}
	client.RoomID = ""
}

func (h *Hub) handleReconnect(client *Client, payload ReconnectPayload) {
	// Find the room this session belongs to
	// For now, the session token maps back via the room manager
	slog.Info("reconnect attempt", "session_token", payload.SessionToken)
	// This will be handled by the room manager
}

// BroadcastToRoom sends a message to all clients in a room.
func (h *Hub) BroadcastToRoom(roomID string, data []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, client := range h.clients {
		if client.RoomID == roomID {
			client.Send(data)
		}
	}
}

// SendToPlayer sends a message to a specific player.
func (h *Hub) SendToPlayer(playerID string, data []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, client := range h.clients {
		if client.PlayerID == playerID {
			client.Send(data)
			return
		}
	}
}

func mustMarshal(v interface{}) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}

func unixMillis() int64 {
	return time.Now().UnixMilli()
}
