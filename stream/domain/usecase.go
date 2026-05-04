package domain

import "errors"

var (
	ErrChannelUnknown   = errors.New("unknown channel")
	ErrChannelNotReady  = errors.New("channel not ready")
	ErrSegmentNotFound  = errors.New("segment not found")
	ErrNoSourceMaterial = errors.New("no source material; pre-segment with ffmpeg into the channel directory")
)

type StreamUc interface {
	Run()
	Stop()

	Playlist(kind ChannelKind) (string, error)
	Segment(kind ChannelKind, seq int64) (path string, err error)
	Info(kind ChannelKind) (ChannelInfo, error)
}
