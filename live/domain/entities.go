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

	// classroom (Phase 1 teacher tools)
	SignalChat  SignalType = "chat"  // bidirectional within a stream
	SignalSlide SignalType = "slide" // broadcaster → viewers, replayed on join

	// classroom (Phase 2 raise-hand / promote-to-speaker)
	// Viewer → server
	SignalRaiseHand SignalType = "raise-hand"
	SignalLowerHand SignalType = "lower-hand"
	// Broadcaster → server (with `to: viewerId`)
	SignalAcceptHand    SignalType = "accept-hand"
	SignalRejectHand    SignalType = "reject-hand"
	SignalRevokeSpeaker SignalType = "revoke-speaker"
	// Server → broadcaster
	SignalHandsUpdate SignalType = "hands-update"
	// Server → all participants of a stream
	SignalSpeakerUpdate SignalType = "speaker-update"
	// Server → specific viewer
	SignalHandAccepted   SignalType = "hand-accepted"
	SignalHandRejected   SignalType = "hand-rejected"
	SignalSpeakerRevoked SignalType = "speaker-revoked"
	// WebRTC back-channel (speaker → broadcaster), forwarded by `to`.
	// Distinct from offer/answer/ice so each side can route into the right
	// peer-connection (the existing forward PC vs the back-channel PC).
	SignalBackOffer  SignalType = "back-offer"
	SignalBackAnswer SignalType = "back-answer"
	SignalBackIce    SignalType = "back-ice"
)

// Hand is a pending raise-hand request shown in the broadcaster's queue.
type Hand struct {
	UserId   string `json:"userId"`
	Username string `json:"username"`
	RaisedAt int64  `json:"raisedAt"` // unix milliseconds
}

// Speaker is the currently-promoted student. At most one at a time in
// Phase 2; accepting a new hand auto-revokes the previous speaker.
type Speaker struct {
	UserId   string `json:"userId"`
	Username string `json:"username"`
	Since    int64  `json:"since"` // unix milliseconds
}

// ChatMessage is the payload of a SignalChat message that the server fans out
// to every participant of a stream (broadcaster + viewers).
type ChatMessage struct {
	From string `json:"from"` // username
	Text string `json:"text"`
	At   int64  `json:"at"` // unix milliseconds
}

// SlideState is the payload of a SignalSlide message. The broadcaster pushes
// page changes; new viewers receive the current state on join so they don't
// have to wait for the next page turn.
type SlideState struct {
	URL  string `json:"url"`
	Page int    `json:"page"`
}

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
