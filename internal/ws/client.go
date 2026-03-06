package ws

import (
	"log/slog"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 4096
	sendBufferSize = 256
)

// Client represents a single WebSocket connection.
type Client struct {
	hub       *Hub
	conn      *websocket.Conn
	send      chan []byte
	PlayerID  string
	SessionID string
	RoomID    string
	mu        sync.Mutex
}

// NewClient creates a new client for the given connection.
func NewClient(hub *Hub, conn *websocket.Conn, playerID, sessionID string) *Client {
	return &Client{
		hub:       hub,
		conn:      conn,
		send:      make(chan []byte, sendBufferSize),
		PlayerID:  playerID,
		SessionID: sessionID,
	}
}

// ReadPump reads messages from the WebSocket connection and forwards them to the hub.
func (c *Client) ReadPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				slog.Error("websocket read error", "player", c.PlayerID, "error", err)
			}
			break
		}

		msg, err := ParseMessage(data)
		if err != nil {
			slog.Warn("invalid message", "player", c.PlayerID, "error", err)
			continue
		}

		c.hub.HandleMessage(c, msg)
	}
}

// WritePump writes messages from the send channel to the WebSocket connection.
func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Batch any queued messages
			n := len(c.send)
			for i := 0; i < n; i++ {
				w.Write([]byte("\n"))
				w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// Send queues a message to be sent to this client.
func (c *Client) Send(data []byte) {
	select {
	case c.send <- data:
	default:
		slog.Warn("client send buffer full, dropping message", "player", c.PlayerID)
	}
}

// Close gracefully closes the client connection.
func (c *Client) Close() {
	close(c.send)
}
