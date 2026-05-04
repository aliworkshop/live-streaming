package call

import (
	"log"
	"net/http"

	"github.com/aliworkshop/live-streaming/call/delivery"
	"github.com/aliworkshop/live-streaming/call/domain"
	"github.com/aliworkshop/live-streaming/call/usecase"
	"github.com/aliworkshop/live-streaming/user/auth"
)

type Module struct {
	Uc        domain.CallUc
	Subscribe http.HandlerFunc
}

func New(logger *log.Logger, token auth.Tokener) *Module {
	uc := usecase.New(logger)
	h := delivery.NewSignalingHandler(uc, token, logger)
	return &Module{
		Uc:        uc,
		Subscribe: h.Subscribe,
	}
}

func (m *Module) Run() { m.Uc.Run() }

func (m *Module) Stop() { m.Uc.Stop() }
