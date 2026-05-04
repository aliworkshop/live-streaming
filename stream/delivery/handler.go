package delivery

import (
	"errors"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/aliworkshop/live-streaming/stream/domain"
)

type Handler struct {
	uc domain.StreamUc
}

func New(uc domain.StreamUc) *Handler {
	return &Handler{uc: uc}
}

func (h *Handler) TvPlaylist(w http.ResponseWriter, _ *http.Request) {
	h.servePlaylist(w, domain.ChannelTv)
}

func (h *Handler) TvSegment(w http.ResponseWriter, r *http.Request) {
	h.serveSegment(w, r, domain.ChannelTv)
}

func (h *Handler) FmPlaylist(w http.ResponseWriter, _ *http.Request) {
	h.servePlaylist(w, domain.ChannelFm)
}

func (h *Handler) FmSegment(w http.ResponseWriter, r *http.Request) {
	h.serveSegment(w, r, domain.ChannelFm)
}

func (h *Handler) servePlaylist(w http.ResponseWriter, kind domain.ChannelKind) {
	pl, err := h.uc.Playlist(kind)
	if err != nil {
		statusForStreamErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_, _ = w.Write([]byte(pl))
}

func (h *Handler) serveSegment(w http.ResponseWriter, r *http.Request, kind domain.ChannelKind) {
	// URL: /stream/<kind>/<seq><.ext>
	base := path.Base(r.URL.Path)
	dot := strings.LastIndex(base, ".")
	var seqStr string
	if dot < 0 {
		seqStr = base
	} else {
		seqStr = base[:dot]
	}
	seq, err := strconv.ParseInt(seqStr, 10, 64)
	if err != nil {
		http.Error(w, "bad segment id", http.StatusBadRequest)
		return
	}
	p, err := h.uc.Segment(kind, seq)
	if err != nil {
		statusForStreamErr(w, err)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	http.ServeFile(w, r, p)
}

func statusForStreamErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrChannelUnknown):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, domain.ErrChannelNotReady):
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
	case errors.Is(err, domain.ErrSegmentNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	default:
		http.Error(w, "stream error", http.StatusInternalServerError)
	}
}
