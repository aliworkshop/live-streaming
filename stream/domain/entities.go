package domain

type Segment struct {
	Name          string
	Duration      float64
	Discontinuity bool
}

type ChannelKind string

const (
	ChannelTv ChannelKind = "tv"
	ChannelFm ChannelKind = "fm"
)

type ChannelInfo struct {
	Kind       ChannelKind
	TotalSecs  float64
	NumSegs    int
	TargetDur  int
	MediaSeq   int64
	WindowSize int
}
