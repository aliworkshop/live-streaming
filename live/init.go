package live

import (
	"log"
	"net/http"

	"github.com/aliworkshop/live-streaming/live/delivery"
	"github.com/aliworkshop/live-streaming/live/domain"
	"github.com/aliworkshop/live-streaming/live/usecase"
	"github.com/aliworkshop/live-streaming/user/auth"
)

type Config struct {
	UploadDir   string
	MaxUploadMB int
}

type Module struct {
	Uc        domain.LiveUc
	Subscribe http.HandlerFunc
	List      http.HandlerFunc
	Upload    http.HandlerFunc
}

func New(logger *log.Logger, token auth.Tokener, cfg Config) *Module {
	uc := usecase.New(logger)
	h := delivery.New(uc, token, logger)
	up := delivery.NewUploadHandler(cfg.UploadDir, cfg.MaxUploadMB, logger)
	return &Module{
		Uc:        uc,
		Subscribe: h.Subscribe,
		List:      h.List,
		Upload:    up.Upload,
	}
}

func (m *Module) Run() { m.Uc.Run() }

func (m *Module) Stop() { m.Uc.Stop() }
