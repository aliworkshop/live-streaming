package app

import (
	"net/http"
	"path/filepath"
)

func (a *App) RegisterRoutes() {
	// Auth / users
	a.mux.HandleFunc("/api/signup", a.UserModule.Signup)
	a.mux.HandleFunc("/api/login", a.UserModule.Login)
	a.mux.HandleFunc("/api/me", a.UserModule.AuthMiddleware(a.UserModule.Me))
	a.mux.HandleFunc("/api/users", a.UserModule.AuthMiddleware(a.UserModule.List))

	// Call (WebRTC signaling websocket)
	a.mux.HandleFunc("/ws/call", a.CallModule.Subscribe)

	// Live broadcast (WebRTC fan-out via signaling websocket)
	a.mux.HandleFunc("/ws/live", a.LiveModule.Subscribe)
	a.mux.HandleFunc("/api/live", a.UserModule.AuthMiddleware(a.LiveModule.List))
	a.mux.HandleFunc("/api/live/upload", a.UserModule.AuthMiddleware(a.LiveModule.Upload))

	// User-uploaded files (PDFs etc.) served from disk
	uploadsDir, _ := filepath.Abs(a.config.Uploads.Dir)
	a.mux.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir(uploadsDir))))

	// 24/7 streams
	a.mux.HandleFunc("/stream/tv.m3u8", a.StreamModule.TvPlaylist)
	a.mux.HandleFunc("/stream/tv/", a.StreamModule.TvSegment)
	a.mux.HandleFunc("/stream/fm.m3u8", a.StreamModule.FmPlaylist)
	a.mux.HandleFunc("/stream/fm/", a.StreamModule.FmSegment)

	// External HLS proxy (one master playlist + opaque-token resource path
	// per channel; see stream/external/proxy.go).
	a.mux.HandleFunc("/api/external", a.ExternalModule.List)
	for _, ch := range a.config.ExternalChannels {
		name := ch.Name
		a.mux.HandleFunc("/stream/"+name+".m3u8", a.ExternalModule.Playlist(name))
		a.mux.HandleFunc("/stream/"+name+"/", a.ExternalModule.Resource(name))
	}

	// Static web client
	webDir, _ := filepath.Abs(a.config.WebDir)
	fs := http.FileServer(http.Dir(webDir))
	a.mux.Handle("/", fs)
}
