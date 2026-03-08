package room

import (
	"math/rand"
	"sync"

	"github.com/ludo/server/internal/wallet"
	"github.com/ludo/server/internal/ws"
	"github.com/ludo/server/pkg/models"
)

// Manager handles room lifecycle: creation, lookup, and cleanup.
type Manager struct {
	rooms     map[string]*Room // code -> Room
	hub       *ws.Hub
	walletSvc *wallet.Service
	mu        sync.RWMutex
}

// NewManager creates a new room manager.
func NewManager(hub *ws.Hub, walletSvc *wallet.Service) *Manager {
	return &Manager{
		rooms:     make(map[string]*Room),
		hub:       hub,
		walletSvc: walletSvc,
	}
}

// CreateRoom creates a new game room and returns its code.
func (m *Manager) CreateRoom(hostID, hostName string, settings models.RoomSettings) (*Room, string) {
	code := m.generateUniqueCode()

	room := NewRoom(code, hostID, hostName, settings, m.hub, m.walletSvc)

	m.mu.Lock()
	m.rooms[code] = room
	m.mu.Unlock()

	// Register with WebSocket hub
	m.hub.RegisterRoom(code, room)

	// Start room goroutine
	go room.Run()

	return room, code
}

// GetRoom returns a room by code.
func (m *Manager) GetRoom(code string) (*Room, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	room, ok := m.rooms[code]
	return room, ok
}

// RemoveRoom removes a room from the manager.
func (m *Manager) RemoveRoom(code string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rooms, code)
	m.hub.UnregisterRoom(code)
}

// ListRooms returns info about all waiting rooms.
func (m *Manager) ListRooms() []models.RoomInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var infos []models.RoomInfo
	for _, room := range m.rooms {
		info := room.GetRoomInfo()
		if info.Status == models.RoomWaiting {
			infos = append(infos, *info)
		}
	}
	return infos
}

// RoomCount returns the number of active rooms.
func (m *Manager) RoomCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.rooms)
}

func (m *Manager) generateUniqueCode() string {
	const chars = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // Avoid O/0/I/1 confusion
	for {
		code := make([]byte, 6)
		for i := range code {
			code[i] = chars[rand.Intn(len(chars))]
		}
		codeStr := string(code)

		m.mu.RLock()
		_, exists := m.rooms[codeStr]
		m.mu.RUnlock()

		if !exists {
			return codeStr
		}
	}
}
