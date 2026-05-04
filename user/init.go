package user

import (
	"log"
	"net/http"

	"github.com/aliworkshop/live-streaming/user/auth"
	"github.com/aliworkshop/live-streaming/user/delivery"
	"github.com/aliworkshop/live-streaming/user/domain"
	"github.com/aliworkshop/live-streaming/user/repository"
	"github.com/aliworkshop/live-streaming/user/usecase"
)

type Module struct {
	Uc      domain.UserUc
	handler *delivery.Handler

	Signup http.HandlerFunc
	Login  http.HandlerFunc
	Me     http.HandlerFunc
	List   http.HandlerFunc
}

func New(logger *log.Logger, token auth.Tokener) *Module {
	repo := repository.NewMemory()
	uc := usecase.New(repo, token)
	h := delivery.New(uc, token, logger)
	return &Module{
		Uc:      uc,
		handler: h,
		Signup:  h.Signup,
		Login:   h.Login,
		Me:      h.Me,
		List:    h.List,
	}
}

func (m *Module) AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return m.handler.AuthMiddleware(next)
}
