package game

import (
	"crypto/rand"
	"math/big"
)

// RollDice generates a cryptographically fair dice roll (1-6).
// Uses rejection sampling via crypto/rand to avoid modulo bias.
func RollDice() (int, error) {
	max := big.NewInt(6)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return 0, err
	}
	return int(n.Int64()) + 1, nil
}
