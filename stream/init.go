package stream

import (
	"log"
	"net/http"

	"github.com/aliworkshop/live-streaming/stream/delivery"
	"github.com/aliworkshop/live-streaming/stream/domain"
	"github.com/aliworkshop/live-streaming/stream/usecase"
)

type Config struct {
	MediaDir string
}

type Module struct {
	Uc domain.StreamUc

	TvPlaylist http.HandlerFunc
	TvSegment  http.HandlerFunc
	FmPlaylist http.HandlerFunc
	FmSegment  http.HandlerFunc
}

func New(logger *log.Logger, cfg Config) *Module {
	uc := usecase.New(logger, cfg.MediaDir)
	h := delivery.New(uc)
	return &Module{
		Uc:         uc,
		TvPlaylist: h.TvPlaylist,
		TvSegment:  h.TvSegment,
		FmPlaylist: h.FmPlaylist,
		FmSegment:  h.FmSegment,
	}
}

func (m *Module) Run() { m.Uc.Run() }

func (m *Module) Stop() { m.Uc.Stop() }
