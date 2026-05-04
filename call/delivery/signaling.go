package delivery

import (
	"log"
	"net/http"

	"github.com/aliworkshop/live-streaming/call/domain"
	"github.com/aliworkshop/live-streaming/user/auth"
	"github.com/gorilla/websocket"
)

type SignalingHandler struct {
	uc       domain.CallUc
	token    auth.Tokener
	logger   *log.Logger
	upgrader websocket.Upgrader
}

func NewSignalingHandler(uc domain.CallUc, token auth.Tokener, logger *log.Logger) *SignalingHandler {
	return &SignalingHandler{
		uc:     uc,
		token:  token,
		logger: logger,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

func (h *SignalingHandler) Subscribe(w http.ResponseWriter, r *http.Request) {
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
		h.logger.Printf("call: ws upgrade error: %v", err)
		return
	}

	h.uc.Register(claims.UserId, claims.Username, conn)
}
