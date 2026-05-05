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

type chatInPayload struct {
	Text string `json:"text"`
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

func decodeChat(p json.RawMessage) chatInPayload {
	var v chatInPayload
	_ = json.Unmarshal(p, &v)
	return v
}

func decodeSlide(p json.RawMessage) domain.SlideState {
	var v domain.SlideState
	_ = json.Unmarshal(p, &v)
	return v
}

func encodeChat(m domain.ChatMessage) json.RawMessage {
	b, _ := json.Marshal(m)
	return b
}

func encodeSlide(s domain.SlideState) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}

func encodeHands(hs []domain.Hand) json.RawMessage {
	b, _ := json.Marshal(map[string]any{"hands": hs})
	return b
}

func encodeSpeaker(sp *domain.Speaker) json.RawMessage {
	// nil renders as `{"speaker": null}` which the client uses to clear UI.
	b, _ := json.Marshal(map[string]any{"speaker": sp})
	return b
}
