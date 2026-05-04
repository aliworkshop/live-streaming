package domain

import "encoding/json"

type SignalType string

const (
	// client → server
	SignalGoLive   SignalType = "go-live"
	SignalStopLive SignalType = "stop-live"
	SignalWatch    SignalType = "watch"
	SignalLeave    SignalType = "leave"

	// WebRTC, forwarded by `to`
	SignalOffer  SignalType = "offer"
	SignalAnswer SignalType = "answer"
	SignalIce    SignalType = "ice"

	// server → client
	SignalRegistered    SignalType = "registered"
	SignalLiveList      SignalType = "live-list"
	SignalViewerJoined  SignalType = "viewer-joined"
	SignalViewerLeft    SignalType = "viewer-left"
	SignalStreamOffline SignalType = "stream-offline"
	SignalError         SignalType = "error"
)

type Signal struct {
	Type    SignalType      `json:"type"`
	From    string          `json:"from,omitempty"`
	To      string          `json:"to,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type StreamSummary struct {
	StreamId    string `json:"streamId"`
	Broadcaster string `json:"broadcaster"`
	Title       string `json:"title,omitempty"`
	Viewers     int    `json:"viewers"`
}
