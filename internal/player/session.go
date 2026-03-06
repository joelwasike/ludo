package player

import (
	"sync"
	"time"
)

// Session tracks a player's connection and room state.
type Session struct {
	PlayerID       string    `json:"player_id"`
	SessionToken   string    `json:"session_token"`
	DisplayName    string    `json:"display_name"`
	RoomCode       string    `json:"room_code"`
	ConnectedAt    time.Time `json:"connected_at"`
	LastPingAt     time.Time `json:"last_ping_at"`
	IsConnected    bool      `json:"is_connected"`
	ReconnectUntil time.Time `json:"reconnect_until"`
}

// SessionStore manages player sessions.
type SessionStore struct {
	sessions map[string]*Session // sessionToken -> Session
	players  map[string]string   // playerID -> sessionToken
	mu       sync.RWMutex
}

// NewSessionStore creates a new session store.
func NewSessionStore() *SessionStore {
	return &SessionStore{
		sessions: make(map[string]*Session),
		players:  make(map[string]string),
	}
}

// Create creates a new session for a player.
func (s *SessionStore) Create(playerID, sessionToken, name, roomCode string) *Session {
	session := &Session{
		PlayerID:     playerID,
		SessionToken: sessionToken,
		DisplayName:  name,
		RoomCode:     roomCode,
		ConnectedAt:  time.Now(),
		LastPingAt:   time.Now(),
		IsConnected:  true,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sessionToken] = session
	s.players[playerID] = sessionToken
	return session
}

// GetByToken retrieves a session by its token.
func (s *SessionStore) GetByToken(token string) (*Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.sessions[token]
	return session, ok
}

// GetByPlayerID retrieves a session by player ID.
func (s *SessionStore) GetByPlayerID(playerID string) (*Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	token, ok := s.players[playerID]
	if !ok {
		return nil, false
	}
	session, ok := s.sessions[token]
	return session, ok
}

// MarkDisconnected marks a session as disconnected with a grace period.
func (s *SessionStore) MarkDisconnected(playerID string, gracePeriod time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	token, ok := s.players[playerID]
	if !ok {
		return
	}
	session, ok := s.sessions[token]
	if !ok {
		return
	}
	session.IsConnected = false
	session.ReconnectUntil = time.Now().Add(gracePeriod)
}

// MarkReconnected marks a session as connected again.
func (s *SessionStore) MarkReconnected(playerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	token, ok := s.players[playerID]
	if !ok {
		return
	}
	session, ok := s.sessions[token]
	if !ok {
		return
	}
	session.IsConnected = true
	session.LastPingAt = time.Now()
}

// Remove removes a session.
func (s *SessionStore) Remove(playerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	token, ok := s.players[playerID]
	if !ok {
		return
	}
	delete(s.sessions, token)
	delete(s.players, playerID)
}

// CleanupExpired removes sessions past their reconnection window.
func (s *SessionStore) CleanupExpired() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	var expired []string
	now := time.Now()
	for token, session := range s.sessions {
		if !session.IsConnected && now.After(session.ReconnectUntil) {
			expired = append(expired, session.PlayerID)
			delete(s.sessions, token)
			delete(s.players, session.PlayerID)
		}
	}
	return expired
}
