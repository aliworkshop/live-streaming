package app

import (
	"log"
	"net/http"

	"github.com/aliworkshop/configer"

	"github.com/aliworkshop/live-streaming/call"
	"github.com/aliworkshop/live-streaming/live"
	"github.com/aliworkshop/live-streaming/stream"
	"github.com/aliworkshop/live-streaming/user"
	"github.com/aliworkshop/live-streaming/user/auth"
)

type App struct {
	registry configer.Registry
	config   config

	logger *log.Logger
	mux    *http.ServeMux
	server *http.Server
	jwt    auth.Tokener

	cloudflaredStop func()
	publicURL       string

	UserModule   *user.Module
	CallModule   *call.Module
	StreamModule *stream.Module
	LiveModule   *live.Module
}

func New(registry configer.Registry) *App {
	return &App{registry: registry}
}

func (a *App) Init() {
	a.initConfig()
	a.initLogger()
	a.initEngine()
}

func (a *App) InitServices() {
	a.initJwt()
	a.initTunnel()
}

func (a *App) InitModules() {
	a.initUserModule()
	a.initCallModule()
	a.initStreamModule()
	a.initLiveModule()
}

func (a *App) panicOnErr(err error) {
	if err != nil {
		panic(err)
	}
}
