package delivery

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/aliworkshop/live-streaming/live/domain"
	"github.com/aliworkshop/live-streaming/user/auth"
	"github.com/gorilla/websocket"
)

type Handler struct {
	uc       domain.LiveUc
	token    auth.Tokener
	logger   *log.Logger
	upgrader websocket.Upgrader
}

func New(uc domain.LiveUc, token auth.Tokener, logger *log.Logger) *Handler {
	return &Handler{
		uc:     uc,
		token:  token,
		logger: logger,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

// Subscribe upgrades the request to a WebSocket and registers the user with
// the live hub.
func (h *Handler) Subscribe(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}
	claims, err := h.token.Verify(token)
	if err != nil {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Printf("live: ws upgrade error: %v", err)
		return
	}
	h.uc.Register(claims.UserId, claims.Username, conn)
}

// List returns the current set of active streams as JSON.
func (h *Handler) List(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(h.uc.Streams())
}
