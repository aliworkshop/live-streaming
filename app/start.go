package app

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

func (a *App) Start() {
	a.RegisterRoutes()

	go a.StreamModule.Run()
	go a.CallModule.Run()
	go a.LiveModule.Run()

	go func() {
		a.logger.Printf("server is running on %s", a.config.Http.Address)
		if a.publicURL != "" {
			a.logger.Printf("public URL (HTTPS): %s", a.publicURL)
		}
		if err := a.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			panic(err)
		}
	}()

	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGTERM, syscall.SIGINT)
	<-c
	a.Stop()
}

func (a *App) Stop() {
	a.logger.Printf("shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), a.config.Http.GracefullyShutdownTimeout)
	defer cancel()
	if err := a.server.Shutdown(ctx); err != nil {
		a.logger.Printf("engine shutdown error: %v", err)
	}
	a.CallModule.Stop()
	a.StreamModule.Stop()
	a.LiveModule.Stop()
	if a.cloudflaredStop != nil {
		a.logger.Printf("stopping cloudflared tunnel")
		a.cloudflaredStop()
	}
	a.logger.Printf("shutdown")
}
