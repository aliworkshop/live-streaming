package live

import (
	"log"
	"net/http"

	"github.com/aliworkshop/live-streaming/live/delivery"
	"github.com/aliworkshop/live-streaming/live/domain"
	"github.com/aliworkshop/live-streaming/live/usecase"
	"github.com/aliworkshop/live-streaming/user/auth"
)

type Module struct {
	Uc        domain.LiveUc
	Subscribe http.HandlerFunc
	List      http.HandlerFunc
}

func New(logger *log.Logger, token auth.Tokener) *Module {
	uc := usecase.New(logger)
	h := delivery.New(uc, token, logger)
	return &Module{
		Uc:        uc,
		Subscribe: h.Subscribe,
		List:      h.List,
	}
}

func (m *Module) Run() { m.Uc.Run() }

func (m *Module) Stop() { m.Uc.Stop() }
