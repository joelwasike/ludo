package server

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/gorilla/websocket"
	"github.com/google/uuid"
	"github.com/ludo/server/internal/config"
	"github.com/ludo/server/internal/payment"
	"github.com/ludo/server/internal/room"
	"github.com/ludo/server/internal/storage"
	"github.com/ludo/server/internal/wallet"
	"github.com/ludo/server/internal/ws"
)

// Server is the main HTTP/WebSocket server.
type Server struct {
	cfg           *config.Config
	hub           *ws.Hub
	roomManager   *room.Manager
	db            *storage.DB
	walletSvc     *wallet.Service
	paymentSvc    *payment.Service
	walletHandler *WalletHandler
	router        chi.Router
	upgrader      websocket.Upgrader
}

// New creates a new server.
func New(cfg *config.Config, db *storage.DB) *Server {
	hub := ws.NewHub()
	walletSvc := wallet.NewService(db.Conn())
	paymentSvc := payment.NewService(cfg.CallbackBaseURL)
	roomManager := room.NewManager(hub, walletSvc)

	s := &Server{
		cfg:           cfg,
		hub:           hub,
		roomManager:   roomManager,
		db:            db,
		walletSvc:     walletSvc,
		paymentSvc:    paymentSvc,
		walletHandler: NewWalletHandler(walletSvc, paymentSvc),
		router:        chi.NewRouter(),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
	}

	s.setupRoutes()
	return s
}

func (s *Server) setupRoutes() {
	r := s.router

	// Middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(corsMiddleware(s.cfg.AllowOrigins))

	// WebSocket endpoint
	r.Get("/ws", s.handleWebSocket)

	// REST API
	r.Route("/api", func(r chi.Router) {
		r.Get("/health", s.handleHealth)

		r.Route("/rooms", func(r chi.Router) {
			r.Post("/", s.handleCreateRoom)
			r.Get("/{code}", s.handleGetRoom)
		})

		r.Get("/stats/{playerID}", s.handleGetStats)

		// Wallet endpoints
		r.Route("/wallet", func(r chi.Router) {
			r.Get("/", s.walletHandler.HandleGetBalance)
			r.Get("/transactions", s.walletHandler.HandleGetTransactions)
			r.Post("/deposit", s.walletHandler.HandleDeposit)
			r.Post("/withdraw", s.walletHandler.HandleWithdraw)
			r.Get("/stake-tiers", s.walletHandler.HandleStakeTiers)
		})

		// Payment callbacks (no auth)
		r.Route("/payment/callback", func(r chi.Router) {
			r.Post("/mpesa", s.walletHandler.HandleMpesaCallback)
			r.Post("/usdt", s.walletHandler.HandleUSDTCallback)
		})
	})
}

// Start starts the HTTP server.
func (s *Server) Start() error {
	go s.hub.Run()

	addr := s.cfg.Addr()
	slog.Info("server starting", "addr", addr)
	return http.ListenAndServe(addr, s.router)
}

func corsMiddleware(allowOrigins string) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", allowOrigins)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Player-ID")

			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("websocket upgrade failed", "error", err)
		return
	}

	playerID := uuid.New().String()
	sessionID := uuid.New().String()

	client := ws.NewClient(s.hub, conn, playerID, sessionID)
	s.hub.RegisterClient(client)

	data, _ := ws.NewMessage("connected", map[string]string{
		"player_id":  playerID,
		"session_id": sessionID,
	})
	client.Send(data)

	go client.WritePump()
	go client.ReadPump()
}
