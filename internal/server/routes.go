package server

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/ludo/server/pkg/models"
)

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":     "ok",
		"rooms":      s.roomManager.RoomCount(),
	})
}

type createRoomRequest struct {
	PlayerName string `json:"player_name"`
	MaxPlayers int    `json:"max_players"`
}

func (s *Server) handleCreateRoom(w http.ResponseWriter, r *http.Request) {
	var req createRoomRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if req.PlayerName == "" {
		req.PlayerName = "Player"
	}
	if req.MaxPlayers < 2 || req.MaxPlayers > 4 {
		req.MaxPlayers = 4
	}

	// The actual player ID will be assigned on WebSocket connection
	// For REST, we generate a temporary host ID
	hostID := "pending-" + req.PlayerName

	settings := models.RoomSettings{
		MaxPlayers:     req.MaxPlayers,
		TurnTimeoutSec: s.cfg.TurnTimeoutSec,
		AllowBots:      true,
	}

	_, code := s.roomManager.CreateRoom(hostID, req.PlayerName, settings)

	writeJSON(w, http.StatusCreated, map[string]string{
		"code": code,
	})
}

func (s *Server) handleGetRoom(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	room, ok := s.roomManager.GetRoom(code)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "room not found"})
		return
	}

	writeJSON(w, http.StatusOK, room.GetRoomInfo())
}

func (s *Server) handleGetStats(w http.ResponseWriter, r *http.Request) {
	playerID := chi.URLParam(r, "playerID")
	stats, err := s.db.GetPlayerStats(playerID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "player not found"})
		return
	}

	writeJSON(w, http.StatusOK, stats)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
