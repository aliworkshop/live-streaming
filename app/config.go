package app

import (
	"os"
	"time"

	"github.com/aliworkshop/configer"
)

const ServiceName = "live-streaming"

type config struct {
	Name             string
	Http             httpConfig
	Auth             authConfig
	Stream           streamConfig
	ExternalChannels []externalChannelConfig
	Tunnel           tunnelConfig
	Uploads          uploadsConfig
	WebDir           string
}

type externalChannelConfig struct {
	Name string // exposed at /stream/<name>.m3u8 — must not collide with tv/fm
	URL  string // upstream HLS master playlist URL
}

type uploadsConfig struct {
	Dir   string // root for user-uploaded files (PDFs etc.); served at /uploads/
	MaxMB int    // per-file size limit
}

type tunnelConfig struct {
	// UseCloudflared spawns cloudflared at startup so that browser HTTPS is
	// available for remote devices (required for getUserMedia camera/mic).
	// Requires `cloudflared` on PATH.
	UseCloudflared bool

	// Name and Hostname together select a *named* tunnel (stable URL across
	// restarts). Both must be set; otherwise the app falls back to a quick
	// trycloudflare.com tunnel whose URL changes every run.
	//
	// Setup once before launching the app:
	//   cloudflared tunnel login
	//   cloudflared tunnel create <name>
	//   cloudflared tunnel route dns  <name> <hostname>
	Name     string
	Hostname string
}

type httpConfig struct {
	Address                   string
	GracefullyShutdownTimeout time.Duration
}

type authConfig struct {
	JwtSecret    string        // override with JWT_SECRET in production
	AccessExpiry time.Duration // e.g. "24h"
}

type streamConfig struct {
	MediaDir string // root for media/<channel>/[<show>/]index.m3u8
}

func loadConfig(r configer.Registry) (config, error) {
	var c config
	if err := r.Unmarshal(&c); err != nil {
		return c, err
	}
	c.applyEnvOverrides()
	c.applyDefaults()
	return c, nil
}

func (c *config) applyEnvOverrides() {
	if v := os.Getenv("JWT_SECRET"); v != "" {
		c.Auth.JwtSecret = v
	}
	if v := os.Getenv("MEDIA_DIR"); v != "" {
		c.Stream.MediaDir = v
	}
	if v := os.Getenv("WEB_DIR"); v != "" {
		c.WebDir = v
	}
}

func (c *config) applyDefaults() {
	if c.Uploads.Dir == "" {
		c.Uploads.Dir = "uploads"
	}
	if c.Uploads.MaxMB == 0 {
		c.Uploads.MaxMB = 200
	}
	if c.Http.Address == "" {
		c.Http.Address = ":9000"
	}
	if c.Http.GracefullyShutdownTimeout == 0 {
		c.Http.GracefullyShutdownTimeout = 10 * time.Second
	}
	if c.Auth.JwtSecret == "" {
		c.Auth.JwtSecret = "change-me-please"
	}
	if c.Auth.AccessExpiry == 0 {
		c.Auth.AccessExpiry = 24 * time.Hour
	}
	if c.Stream.MediaDir == "" {
		c.Stream.MediaDir = "media"
	}
	if c.WebDir == "" {
		c.WebDir = "web"
	}
}
