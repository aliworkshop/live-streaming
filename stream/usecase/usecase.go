package usecase

import (
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/aliworkshop/live-streaming/stream/domain"
)

type useCase struct {
	logger   *log.Logger
	mediaDir string

	mu       sync.RWMutex
	channels map[domain.ChannelKind]*looper
	started  bool
}

func New(logger *log.Logger, mediaDir string) domain.StreamUc {
	return &useCase{
		logger:   logger,
		mediaDir: mediaDir,
		channels: make(map[domain.ChannelKind]*looper),
	}
}

func (uc *useCase) loadChannel(kind domain.ChannelKind) {
	dir := filepath.Join(uc.mediaDir, string(kind))

	// Single-show mode: media/<kind>/index.m3u8 directly.
	if segs, target, err := loadVodPlaylist(filepath.Join(dir, "index.m3u8")); err == nil {
		l := newLooper(kind, dir, segs, target, uc.logger)
		uc.mu.Lock()
		uc.channels[kind] = l
		uc.mu.Unlock()
		uc.logger.Printf("stream/%s: loaded 1 show, %d segments, total %.1fs",
			kind, len(segs), l.totalSecs)
		return
	}

	// Multi-show mode: media/<kind>/<show>/index.m3u8, alphabetical.
	entries, err := os.ReadDir(dir)
	if err != nil {
		uc.logger.Printf("stream/%s: cannot read %s: %v — channel disabled", kind, dir, err)
		return
	}
	var shows []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err = os.Stat(filepath.Join(dir, e.Name(), "index.m3u8")); err == nil {
			shows = append(shows, e.Name())
		}
	}
	sort.Strings(shows)
	if len(shows) == 0 {
		uc.logger.Printf("stream/%s: no shows under %s — channel disabled", kind, dir)
		return
	}

	var merged []domain.Segment
	targetDur := 0
	for _, show := range shows {
		showDir := filepath.Join(dir, show)
		segs, td, err := loadVodPlaylist(filepath.Join(showDir, "index.m3u8"))
		if err != nil {
			uc.logger.Printf("stream/%s: skipping show %q: %v", kind, show, err)
			continue
		}
		if td > targetDur {
			targetDur = td
		}
		for i, s := range segs {
			merged = append(merged, domain.Segment{
				Name:          filepath.Join(show, s.Name),
				Duration:      s.Duration,
				Discontinuity: i == 0, // boundary at the start of every show (incl. loop wrap)
			})
		}
	}
	if len(merged) == 0 {
		uc.logger.Printf("stream/%s: no usable shows — channel disabled", kind)
		return
	}

	l := newLooper(kind, dir, merged, targetDur, uc.logger)
	uc.mu.Lock()
	uc.channels[kind] = l
	uc.mu.Unlock()
	uc.logger.Printf("stream/%s: loaded %d shows, %d segments, total %.1fs",
		kind, len(shows), len(merged), l.totalSecs)
}

func (uc *useCase) Run() {
	if uc.started {
		return
	}
	uc.started = true
	uc.loadChannel(domain.ChannelTv)
	uc.loadChannel(domain.ChannelFm)

	uc.mu.RLock()
	defer uc.mu.RUnlock()
	for _, l := range uc.channels {
		go l.run()
	}
}

func (uc *useCase) Stop() {
	uc.mu.RLock()
	defer uc.mu.RUnlock()
	for _, l := range uc.channels {
		l.close()
	}
}

func (uc *useCase) Playlist(kind domain.ChannelKind) (string, error) {
	uc.mu.RLock()
	l, ok := uc.channels[kind]
	uc.mu.RUnlock()
	if !ok {
		return "", domain.ErrChannelUnknown
	}
	pl := l.playlist()
	if pl == "" {
		return "", domain.ErrChannelNotReady
	}
	return pl, nil
}

func (uc *useCase) Segment(kind domain.ChannelKind, seq int64) (string, error) {
	uc.mu.RLock()
	l, ok := uc.channels[kind]
	uc.mu.RUnlock()
	if !ok {
		return "", domain.ErrChannelUnknown
	}
	return l.segmentPath(seq)
}

func (uc *useCase) Info(kind domain.ChannelKind) (domain.ChannelInfo, error) {
	uc.mu.RLock()
	l, ok := uc.channels[kind]
	uc.mu.RUnlock()
	if !ok {
		return domain.ChannelInfo{}, domain.ErrChannelUnknown
	}
	return l.info(), nil
}
