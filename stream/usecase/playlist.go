package usecase

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/aliworkshop/live-streaming/stream/domain"
)

// loadVodPlaylist parses an ffmpeg-generated HLS VOD playlist and returns its
// segment list. Lines we recognize:
//
//	#EXT-X-TARGETDURATION:<int>
//	#EXTINF:<float>,
//	<filename>
func loadVodPlaylist(path string) ([]domain.Segment, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()

	var (
		segs      []domain.Segment
		targetDur int
		nextDur   float64
		expectURI bool
	)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<16), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "#EXT-X-TARGETDURATION:"):
			v := strings.TrimPrefix(line, "#EXT-X-TARGETDURATION:")
			td, err := strconv.Atoi(strings.TrimSpace(v))
			if err == nil {
				targetDur = td
			}
		case strings.HasPrefix(line, "#EXTINF:"):
			v := strings.TrimPrefix(line, "#EXTINF:")
			v = strings.TrimSuffix(v, ",")
			parts := strings.SplitN(v, ",", 2)
			d, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
			if err != nil {
				return nil, 0, fmt.Errorf("bad EXTINF %q: %w", line, err)
			}
			nextDur = d
			expectURI = true
		case strings.HasPrefix(line, "#"):
			// other tags are ignored for live windowing
		default:
			if expectURI {
				segs = append(segs, domain.Segment{Name: line, Duration: nextDur})
				expectURI = false
				nextDur = 0
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, 0, err
	}
	if len(segs) == 0 {
		return nil, 0, fmt.Errorf("no segments in playlist %s", path)
	}
	if targetDur == 0 {
		// derive from longest segment, rounded up
		max := 0.0
		for _, s := range segs {
			if s.Duration > max {
				max = s.Duration
			}
		}
		targetDur = int(max + 0.999)
		if targetDur == 0 {
			targetDur = 1
		}
	}
	return segs, targetDur, nil
}
