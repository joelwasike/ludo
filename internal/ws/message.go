package ws

import "encoding/json"

// Message is the base envelope for all WebSocket messages.
type Message struct {
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	Timestamp int64           `json:"timestamp,omitempty"`
	Seq       int             `json:"seq,omitempty"`
}

// ParseMessage parses a raw JSON byte slice into a Message.
func ParseMessage(data []byte) (*Message, error) {
	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// NewMessage creates a new message with the given type and payload.
func NewMessage(msgType string, payload interface{}) ([]byte, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	msg := Message{
		Type:    msgType,
		Payload: payloadBytes,
	}
	return json.Marshal(msg)
}

// NewErrorMessage creates an error message.
func NewErrorMessage(code, message string) ([]byte, error) {
	return NewMessage("error", map[string]string{
		"code":    code,
		"message": message,
	})
}

// --- Client message payloads ---

type JoinRoomPayload struct {
	Code       string `json:"code"`
	PlayerName string `json:"player_name"`
}

type MoveTokenPayload struct {
	TokenID int `json:"token_id"`
}

type AddBotPayload struct {
	Difficulty string `json:"difficulty"`
}

type RemoveBotPayload struct {
	Color int `json:"color"`
}

type ReconnectPayload struct {
	SessionToken string `json:"session_token"`
	LastSeq      int    `json:"last_seq"`
}
