package app

import (
	"github.com/aliworkshop/live-streaming/call"
	"github.com/aliworkshop/live-streaming/live"
	"github.com/aliworkshop/live-streaming/stream"
	"github.com/aliworkshop/live-streaming/user"
)

func (a *App) initUserModule() {
	a.UserModule = user.New(a.logger, a.jwt)
}

func (a *App) initCallModule() {
	a.CallModule = call.New(a.logger, a.jwt)
}

func (a *App) initStreamModule() {
	a.StreamModule = stream.New(a.logger, stream.Config{
		MediaDir: a.config.Stream.MediaDir,
	})
}

func (a *App) initLiveModule() {
	a.LiveModule = live.New(a.logger, a.jwt)
}
