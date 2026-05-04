package usecase

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/aliworkshop/live-streaming/stream/domain"
)

const (
	defaultWindow = 4
	urlPrefix     = "/stream"
)

// looper drives a single channel: it owns the segment list, advances a
// monotonic media sequence in real time, and exposes a sliding-window playlist
// for HLS clients.
type looper struct {
	kind      domain.ChannelKind
	dir       string
	segments  []domain.Segment
	targetDur int
	totalSecs float64
	urlBase   string
	logger    *log.Logger

	mu       sync.RWMutex
	mediaSeq int64
	window   []int64
	stop     chan struct{}
	stopped  bool
	ready    bool
}

func newLooper(kind domain.ChannelKind, dir string, segments []domain.Segment, targetDur int, logger *log.Logger) *looper {
	total := 0.0
	for _, s := range segments {
		total += s.Duration
	}
	return &looper{
		kind:      kind,
		dir:       dir,
		segments:  segments,
		targetDur: targetDur,
		totalSecs: total,
		urlBase:   fmt.Sprintf("%s/%s", urlPrefix, kind),
		logger:    logger,
		stop:      make(chan struct{}),
		ready:     true,
	}
}

func (l *looper) run() {
	if !l.ready {
		return
	}
	// Seed the sliding window so a client connecting at t=0 has segments
	// available without waiting one tick.
	l.mu.Lock()
	for i := 0; i < defaultWindow; i++ {
		l.window = append(l.window, l.mediaSeq)
		l.mediaSeq++
	}
	l.mu.Unlock()

	timer := time.NewTimer(l.curSegmentDuration())
	defer timer.Stop()
	for {
		select {
		case <-l.stop:
			return
		case <-timer.C:
			l.advance()
			timer.Reset(l.curSegmentDuration())
		}
	}
}

func (l *looper) curSegmentDuration() time.Duration {
	l.mu.RLock()
	idx := int(l.mediaSeq % int64(len(l.segments)))
	d := l.segments[idx].Duration
	l.mu.RUnlock()
	if d <= 0 {
		d = float64(l.targetDur)
	}
	return time.Duration(d * float64(time.Second))
}

func (l *looper) advance() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.window = append(l.window, l.mediaSeq)
	l.mediaSeq++
	if len(l.window) > defaultWindow {
		l.window = l.window[len(l.window)-defaultWindow:]
	}
}

func (l *looper) close() {
	l.mu.Lock()
	if l.stopped {
		l.mu.Unlock()
		return
	}
	l.stopped = true
	close(l.stop)
	l.mu.Unlock()
}

func (l *looper) playlist() string {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if len(l.window) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintln(&b, "#EXTM3U")
	fmt.Fprintln(&b, "#EXT-X-VERSION:3")
	fmt.Fprintf(&b, "#EXT-X-TARGETDURATION:%d\n", l.targetDur)
	fmt.Fprintf(&b, "#EXT-X-MEDIA-SEQUENCE:%d\n", l.window[0])
	for _, seq := range l.window {
		idx := int(seq % int64(len(l.segments)))
		seg := l.segments[idx]
		if seg.Discontinuity {
			fmt.Fprintln(&b, "#EXT-X-DISCONTINUITY")
		}
		fmt.Fprintf(&b, "#EXTINF:%.3f,\n", seg.Duration)
		fmt.Fprintf(&b, "%s/%d%s\n", l.urlBase, seq, filepath.Ext(seg.Name))
	}
	return b.String()
}

func (l *looper) segmentPath(seq int64) (string, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if !l.ready || len(l.segments) == 0 {
		return "", domain.ErrChannelNotReady
	}
	// Validate that the requested seq is currently within (or just behind)
	// the live window. We allow a generous backlog so slow clients can still
	// fetch segments they referenced moments ago.
	oldest := l.mediaSeq - int64(defaultWindow*4)
	if seq < oldest || seq >= l.mediaSeq+int64(defaultWindow) {
		return "", domain.ErrSegmentNotFound
	}
	idx := int(seq % int64(len(l.segments)))
	return filepath.Join(l.dir, l.segments[idx].Name), nil
}

func (l *looper) info() domain.ChannelInfo {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return domain.ChannelInfo{
		Kind:       l.kind,
		TotalSecs:  l.totalSecs,
		NumSegs:    len(l.segments),
		TargetDur:  l.targetDur,
		MediaSeq:   l.mediaSeq,
		WindowSize: defaultWindow,
	}
}
