package app

import (
	"github.com/aliworkshop/live-streaming/call"
	"github.com/aliworkshop/live-streaming/live"
	"github.com/aliworkshop/live-streaming/stream"
	"github.com/aliworkshop/live-streaming/stream/external"
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
	a.LiveModule = live.New(a.logger, a.jwt, live.Config{
		UploadDir:   a.config.Uploads.Dir,
		MaxUploadMB: a.config.Uploads.MaxMB,
	})
}

func (a *App) initExternalModule() {
	chs := make([]external.Channel, 0, len(a.config.ExternalChannels))
	for _, c := range a.config.ExternalChannels {
		chs = append(chs, external.Channel{
			Name: c.Name,
			URL:  c.URL,
			Kind: external.ChannelKind(c.Kind),
			Logo: c.Logo,
		})
	}
	a.ExternalModule = external.New(a.logger, chs)
}
