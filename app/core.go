package app

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/aliworkshop/live-streaming/tunnel"
	"github.com/aliworkshop/live-streaming/user/auth"
)

func (a *App) initConfig() {
	cfg, err := loadConfig(a.registry)
	if err != nil {
		panic("cannot load config: " + err.Error())
	}
	a.config = cfg
}

func (a *App) initLogger() {
	a.logger = log.New(os.Stdout, "["+ServiceName+"] ", log.LstdFlags|log.Lshortfile)
}

func (a *App) initEngine() {
	a.mux = http.NewServeMux()
	a.server = &http.Server{
		Addr:              a.config.Http.Address,
		Handler:           a.mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
}

func (a *App) initJwt() {
	a.jwt = auth.NewTokener(a.config.Auth.JwtSecret, a.config.Auth.AccessExpiry)
}

func (a *App) initTunnel() {
	if !a.config.Tunnel.UseCloudflared {
		return
	}
	port, err := portFromAddress(a.config.Http.Address)
	if err != nil {
		a.logger.Printf("cloudflared: skipping (%v)", err)
		return
	}

	var (
		url    string
		stop   func()
		mode   string
		runErr error
	)
	if a.config.Tunnel.Name != "" && a.config.Tunnel.Hostname != "" {
		mode = fmt.Sprintf("named (%s → %s)", a.config.Tunnel.Name, a.config.Tunnel.Hostname)
		a.logger.Printf("starting cloudflared %s on port %d", mode, port)
		url, stop, runErr = tunnel.StartNamedTunnel(context.Background(), port,
			a.config.Tunnel.Name, a.config.Tunnel.Hostname)
	} else {
		mode = "quick"
		a.logger.Printf("starting cloudflared %s tunnel on port %d", mode, port)
		url, stop, runErr = tunnel.StartCloudflared(context.Background(), port)
	}
	if runErr != nil {
		a.logger.Printf("cloudflared %s tunnel: %v", mode, runErr)
		return
	}
	a.cloudflaredStop = stop
	a.publicURL = url
	a.logger.Printf("cloudflared %s tunnel ready: %s", mode, url)
}

func portFromAddress(addr string) (int, error) {
	if addr == "" {
		return 0, fmt.Errorf("empty http address")
	}
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return 0, fmt.Errorf("split host:port %q: %w", addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return 0, fmt.Errorf("parse port %q: %w", portStr, err)
	}
	return port, nil
}
