package usecase

import (
	"encoding/json"

	"github.com/aliworkshop/live-streaming/call/domain"
)

func encodePeers(peers []domain.Peer) (json.RawMessage, error) {
	return json.Marshal(peers)
}
