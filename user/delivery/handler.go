package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/aliworkshop/live-streaming/user/auth"
	"github.com/aliworkshop/live-streaming/user/domain"
)

type ctxKey string

const ClaimsKey ctxKey = "claims"

type Handler struct {
	uc     domain.UserUc
	token  auth.Tokener
	logger *log.Logger
}

func New(uc domain.UserUc, token auth.Tokener, logger *log.Logger) *Handler {
	return &Handler{uc: uc, token: token, logger: logger}
}

type signupReq struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"displayName"`
}

type loginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type tokenResp struct {
	Token string        `json:"token"`
	User  domain.Public `json:"user"`
}

func (h *Handler) Signup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req signupReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	u, tok, err := h.uc.Signup(req.Username, req.Password, req.DisplayName)
	if err != nil {
		statusForUserErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, tokenResp{Token: tok, User: u.Public()})
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req loginReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	u, tok, err := h.uc.Login(req.Username, req.Password)
	if err != nil {
		statusForUserErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tokenResp{Token: tok, User: u.Public()})
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	claims, ok := r.Context().Value(ClaimsKey).(*auth.Claims)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	u, err := h.uc.GetById(claims.UserId)
	if err != nil {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	}
	writeJSON(w, http.StatusOK, u.Public())
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.uc.List())
}

func (h *Handler) AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := bearer(r)
		if token == "" {
			token = r.URL.Query().Get("token")
		}
		if token == "" {
			writeErr(w, http.StatusUnauthorized, "missing token")
			return
		}
		claims, err := h.token.Verify(token)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "invalid token")
			return
		}
		ctx := context.WithValue(r.Context(), ClaimsKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if h == "" {
		return ""
	}
	parts := strings.SplitN(h, " ", 2)
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return parts[1]
	}
	return ""
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func statusForUserErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrUserExists):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, domain.ErrInvalidUsername),
		errors.Is(err, domain.ErrInvalidPassword):
		writeErr(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, domain.ErrInvalidCreds),
		errors.Is(err, domain.ErrUserNotFound):
		writeErr(w, http.StatusUnauthorized, err.Error())
	default:
		writeErr(w, http.StatusInternalServerError, "internal error")
	}
}
