package config

import (
	"os"
	"strconv"
)

// Config holds all server configuration.
type Config struct {
	Host              string
	Port              int
	DatabasePath      string
	MaxRooms          int
	TurnTimeoutSec    int
	ReconnectGraceSec int
	AllowOrigins      string
	CallbackBaseURL   string
}

// Load reads configuration from environment variables with defaults.
func Load() *Config {
	return &Config{
		Host:              getEnv("LUDO_HOST", "0.0.0.0"),
		Port:              getEnvInt("LUDO_PORT", 8096),
		DatabasePath:      getEnv("LUDO_DB_PATH", "./ludo.db"),
		MaxRooms:          getEnvInt("LUDO_MAX_ROOMS", 100),
		TurnTimeoutSec:    getEnvInt("LUDO_TURN_TIMEOUT", 30),
		ReconnectGraceSec: getEnvInt("LUDO_RECONNECT_GRACE", 120),
		AllowOrigins:      getEnv("LUDO_ALLOW_ORIGINS", "*"),
		CallbackBaseURL:   getEnv("LUDO_CALLBACK_URL", "https://ludo-api.chessd.games/api/payment/callback"),
	}
}

// Addr returns the listen address string.
func (c *Config) Addr() string {
	return c.Host + ":" + strconv.Itoa(c.Port)
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}
