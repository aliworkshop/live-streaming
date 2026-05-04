package domain

import "encoding/json"

type SignalType string

const (
	SignalRegistered  SignalType = "registered"
	SignalPeers       SignalType = "peers"
	SignalCallRequest SignalType = "call-request"
	SignalCallAccept  SignalType = "call-accept"
	SignalCallReject  SignalType = "call-reject"
	SignalCallEnd     SignalType = "call-end"
	SignalOffer       SignalType = "offer"
	SignalAnswer      SignalType = "answer"
	SignalIce         SignalType = "ice"
	SignalError       SignalType = "error"
	SignalPeerOffline SignalType = "peer-offline"
)

type Signal struct {
	Type    SignalType      `json:"type"`
	From    string          `json:"from,omitempty"`
	To      string          `json:"to,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type Peer struct {
	UserId   string `json:"userId"`
	Username string `json:"username"`
}
