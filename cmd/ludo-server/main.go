package main

import (
	"log/slog"
	"os"

	"github.com/ludo/server/internal/config"
	"github.com/ludo/server/internal/server"
	"github.com/ludo/server/internal/storage"
)

func main() {
	// Setup structured logging
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	slog.Info("Ludo Server starting...")

	// Load configuration
	cfg := config.Load()

	// Initialize database
	db, err := storage.New(cfg.DatabasePath)
	if err != nil {
		slog.Error("failed to initialize database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	// Create and start server
	srv := server.New(cfg, db)
	if err := srv.Start(); err != nil {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}
