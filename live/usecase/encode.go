package usecase

import (
	"encoding/json"

	"github.com/aliworkshop/live-streaming/live/domain"
)

func encodeStreams(s []domain.StreamSummary) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}

func encodeStreamId(id string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"streamId": id})
	return b
}

type goLivePayload struct {
	Title string `json:"title"`
}

type watchPayload struct {
	StreamId string `json:"streamId"`
}

func decodeGoLive(p json.RawMessage) goLivePayload {
	var v goLivePayload
	_ = json.Unmarshal(p, &v)
	return v
}

func decodeWatch(p json.RawMessage) watchPayload {
	var v watchPayload
	_ = json.Unmarshal(p, &v)
	return v
}
