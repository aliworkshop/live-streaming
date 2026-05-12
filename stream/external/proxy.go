// Package external implements a stateless HLS proxy: it fetches a remote
// master playlist, rewrites every URI (variant playlists, segments, key/map
// URIs) to route through this server, and proxies the resulting traffic.
//
// Because rewrite is fully URL-driven (the upstream URL is base64url-encoded
// into the proxied path), no per-channel state is kept across requests —
// each request resolves its target purely from the path.
package external

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ChannelKind selects how the configured URL is interpreted.
type ChannelKind string

const (
	// KindHLS — URL is a directly-reachable HLS master playlist.
	KindHLS ChannelKind = "hls"
	// KindYouTube — URL is a youtube.com/watch?v=… page; yt-dlp resolves
	// the actual HLS master at request time. Requires `yt-dlp` on PATH.
	KindYouTube ChannelKind = "youtube"
)

type Channel struct {
	Name string
	URL  string
	Kind ChannelKind // empty == KindHLS
	Logo string      // optional URL of an image rendered as a corner overlay
}

func (c Channel) kind() ChannelKind {
	if c.Kind == "" {
		return KindHLS
	}
	return c.Kind
}

type resolvedURL struct {
	url    string
	expiry time.Time
}

// youtubeCacheTTL is how long a yt-dlp-resolved URL is reused before we shell
// out again. Far shorter than YouTube's actual signed-URL lifetime (~6h) so
// network blips and stream restarts heal quickly.
const youtubeCacheTTL = 5 * time.Minute

type Module struct {
	channels map[string]Channel
	client   *http.Client
	logger   *log.Logger

	// Cache of yt-dlp-resolved master URLs (only populated for KindYouTube).
	resolveMu sync.Mutex
	resolved  map[string]resolvedURL
}

func New(logger *log.Logger, channels []Channel) *Module {
	m := make(map[string]Channel, len(channels))
	for _, ch := range channels {
		if ch.Name == "" || ch.URL == "" {
			continue
		}
		m[ch.Name] = ch
	}
	return &Module{
		channels: m,
		client:   &http.Client{Timeout: 30 * time.Second},
		logger:   logger,
		resolved: make(map[string]resolvedURL),
	}
}

// masterURL returns the URL to fetch for a channel's master playlist. For HLS
// channels that's just the configured URL; for YouTube it's the cached
// (or freshly resolved) `.m3u8` extracted by yt-dlp.
func (m *Module) masterURL(ch Channel) (string, error) {
	if ch.kind() != KindYouTube {
		return ch.URL, nil
	}
	m.resolveMu.Lock()
	defer m.resolveMu.Unlock()
	if r, ok := m.resolved[ch.Name]; ok && time.Now().Before(r.expiry) {
		return r.url, nil
	}
	resolved, err := resolveYouTube(ch.URL)
	if err != nil {
		return "", err
	}
	m.resolved[ch.Name] = resolvedURL{url: resolved, expiry: time.Now().Add(youtubeCacheTTL)}
	m.logger.Printf("external/%s: yt-dlp resolved → %s", ch.Name, truncate(resolved, 80))
	return resolved, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ytDlpPath finds yt-dlp on PATH, falling back to standard Homebrew install
// locations on macOS so the server still works when launched from a shell
// (or IDE / daemon) that strips PATH.
func ytDlpPath() (string, error) {
	if p, err := exec.LookPath("yt-dlp"); err == nil {
		return p, nil
	}
	for _, candidate := range []string{
		"/opt/homebrew/bin/yt-dlp", // Apple Silicon Homebrew
		"/usr/local/bin/yt-dlp",    // Intel Homebrew
		"/home/linuxbrew/.linuxbrew/bin/yt-dlp",
	} {
		if _, err := exec.LookPath(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", errors.New("yt-dlp not on PATH and not in any common install location " +
		"(install with `brew install yt-dlp`)")
}

func resolveYouTube(watchURL string) (string, error) {
	binary, err := ytDlpPath()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// `manifest_url` is the multi-variant HLS master (`hls_variant`).
	// `-g -f best…` would instead return a single-rendition `hls_playlist`
	// URL, which works but loses adaptive quality switching — viewers would
	// be stuck on whatever rendition yt-dlp picked.
	cmd := exec.CommandContext(ctx, binary,
		"--no-warnings",
		"--print", "%(manifest_url)s",
		watchURL)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err = cmd.Run(); err != nil {
		return "", fmt.Errorf("yt-dlp failed: %w (stderr: %s)", err, strings.TrimSpace(errBuf.String()))
	}
	first := strings.TrimSpace(strings.SplitN(out.String(), "\n", 2)[0])
	if first == "" || first == "NA" {
		return "", errors.New("yt-dlp returned no manifest_url — the video probably isn't a live stream (VOD has no master playlist)")
	}
	if !strings.Contains(first, ".m3u8") {
		return "", fmt.Errorf("yt-dlp returned non-HLS URL (only HLS sources are supported): %s", truncate(first, 80))
	}
	return first, nil
}

// Channels returns the configured channel list (read-only snapshot).
func (m *Module) Channels() []Channel {
	out := make([]Channel, 0, len(m.channels))
	for _, ch := range m.channels {
		out = append(out, ch)
	}
	return out
}

// List exposes the configured channels as JSON for the web UI.
//
//	GET /api/external -> [{"name":"lenz","url":"/stream/lenz.m3u8","logo":"…"}, ...]
func (m *Module) List(w http.ResponseWriter, _ *http.Request) {
	type item struct {
		Name string `json:"name"`
		URL  string `json:"url"`
		Logo string `json:"logo,omitempty"`
	}
	items := make([]item, 0, len(m.channels))
	for _, ch := range m.channels {
		items = append(items, item{
			Name: ch.Name,
			URL:  "/stream/" + ch.Name + ".m3u8",
			Logo: ch.Logo,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(items)
}

// Playlist returns a handler that serves the master playlist for `name`,
// rewritten so every URI is proxied through this server.
//
//	GET /stream/<name>.m3u8
func (m *Module) Playlist(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ch, ok := m.channels[name]
		if !ok {
			http.Error(w, "unknown channel", http.StatusNotFound)
			return
		}
		master, err := m.masterURL(ch)
		if err != nil {
			m.logger.Printf("external/%s: cannot resolve master: %v", name, err)
			http.Error(w, "cannot resolve channel: "+err.Error(), http.StatusBadGateway)
			return
		}
		m.serve(w, r, master, ch.Name, true)
	}
}

// Resource returns a handler for sub-resources (variant playlists, segments,
// key/map files). The path tail after /stream/<name>/ is a base64url-encoded
// absolute upstream URL.
//
//	GET /stream/<name>/<base64url>
func (m *Module) Resource(name string) http.HandlerFunc {
	prefix := "/stream/" + name + "/"
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := m.channels[name]; !ok {
			http.Error(w, "unknown channel", http.StatusNotFound)
			return
		}
		token := strings.TrimPrefix(r.URL.Path, prefix)
		if token == "" {
			http.Error(w, "missing token", http.StatusBadRequest)
			return
		}
		decoded, err := base64.RawURLEncoding.DecodeString(token)
		if err != nil {
			http.Error(w, "bad token", http.StatusBadRequest)
			return
		}
		upstream := string(decoded)
		// Treat .m3u8 (after stripping query) as a playlist that needs rewriting.
		isPlaylist := strings.HasSuffix(strings.ToLower(stripQuery(upstream)), ".m3u8")
		m.serve(w, r, upstream, name, isPlaylist)
	}
}

func stripQuery(u string) string {
	if i := strings.IndexByte(u, '?'); i >= 0 {
		return u[:i]
	}
	return u
}

func (m *Module) serve(w http.ResponseWriter, r *http.Request, upstream, channelName string, asPlaylist bool) {
	// Merge any query string the client added (e.g. HLS.js's blocking-reload
	// _HLS_msn / _HLS_part) onto the upstream URL — without this, low-latency
	// HLS clients hang waiting for segments that never arrive.
	if r.URL.RawQuery != "" {
		sep := "?"
		if strings.Contains(upstream, "?") {
			sep = "&"
		}
		upstream = upstream + sep + r.URL.RawQuery
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, upstream, nil)
	if err != nil {
		http.Error(w, "bad upstream url", http.StatusBadGateway)
		return
	}
	if rg := r.Header.Get("Range"); rg != "" {
		req.Header.Set("Range", rg)
	}
	if ua := r.Header.Get("User-Agent"); ua != "" {
		req.Header.Set("User-Agent", ua)
	} else {
		req.Header.Set("User-Agent", "live-streaming-proxy/1.0")
	}

	res, err := m.client.Do(req)
	if err != nil {
		m.logger.Printf("external/%s: upstream fetch failed: %v", channelName, err)
		http.Error(w, "upstream fetch failed", http.StatusBadGateway)
		return
	}
	defer res.Body.Close()

	contentType := res.Header.Get("Content-Type")
	isPlaylist := asPlaylist || strings.Contains(contentType, "mpegurl")

	w.Header().Set("Access-Control-Allow-Origin", "*")

	if !isPlaylist || res.StatusCode >= 400 {
		copyResponseHeaders(w.Header(), res.Header)
		w.WriteHeader(res.StatusCode)
		_, _ = io.Copy(w, res.Body)
		return
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		m.logger.Printf("external/%s: read upstream playlist: %v", channelName, err)
		http.Error(w, "upstream read failed", http.StatusBadGateway)
		return
	}
	base, err := url.Parse(upstream)
	if err != nil {
		http.Error(w, "bad upstream url", http.StatusBadGateway)
		return
	}
	rewritten := rewritePlaylist(body, base, channelName)

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(rewritten)
}

func copyResponseHeaders(dst, src http.Header) {
	for k, vs := range src {
		switch strings.ToLower(k) {
		case "connection", "keep-alive", "transfer-encoding", "upgrade",
			"access-control-allow-origin", "content-length":
			continue
		}
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
}

var uriAttrRe = regexp.MustCompile(`URI="([^"]+)"`)

// rewritePlaylist walks an HLS playlist line by line, rewriting URI lines and
// any URI="..." attributes inside tags (EXT-X-KEY, EXT-X-MAP, EXT-X-MEDIA, …)
// so they go through our server.
func rewritePlaylist(body []byte, base *url.URL, channelName string) []byte {
	var out strings.Builder
	out.Grow(len(body) * 2)
	rest := string(body)
	for len(rest) > 0 {
		var line, term string
		if i := strings.IndexByte(rest, '\n'); i < 0 {
			line, rest = rest, ""
		} else {
			line, rest = rest[:i], rest[i+1:]
			term = "\n"
			if strings.HasSuffix(line, "\r") {
				line = line[:len(line)-1]
				term = "\r\n"
			}
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			if strings.Contains(line, `URI="`) {
				line = uriAttrRe.ReplaceAllStringFunc(line, func(m string) string {
					sub := uriAttrRe.FindStringSubmatch(m)
					if len(sub) < 2 {
						return m
					}
					abs, err := base.Parse(sub[1])
					if err != nil {
						return m
					}
					return `URI="` + tokenURL(channelName, abs.String()) + `"`
				})
			}
			out.WriteString(line)
			out.WriteString(term)
			continue
		}
		abs, err := base.Parse(trimmed)
		if err != nil {
			out.WriteString(line)
			out.WriteString(term)
			continue
		}
		out.WriteString(tokenURL(channelName, abs.String()))
		out.WriteString(term)
	}
	return []byte(out.String())
}

func tokenURL(channelName, abs string) string {
	return fmt.Sprintf("/stream/%s/%s",
		channelName,
		base64.RawURLEncoding.EncodeToString([]byte(abs)))
}
