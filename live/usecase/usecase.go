package usecase

import (
	"errors"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/aliworkshop/live-streaming/live/client"
	"github.com/aliworkshop/live-streaming/live/domain"
	"github.com/gorilla/websocket"
)

type liveStream struct {
	broadcasterId string
	broadcaster   string
	title         string
	viewers       map[string]struct{}
	currentSlide  *domain.SlideState     // last slide pushed by the broadcaster, replayed to new viewers
	pendingHands  map[string]domain.Hand // viewerId -> hand state
	speaker       *domain.Speaker        // currently promoted student, at most one
}

type useCase struct {
	logger *log.Logger

	mu       sync.RWMutex
	clients  map[string]*client.Client
	streams  map[string]*liveStream
	watching map[string]string // viewerId -> broadcasterId

	signals chan *domain.Signal
	leave   chan string
	stop    chan struct{}
	started bool
}

func New(logger *log.Logger) domain.LiveUc {
	return &useCase{
		logger:   logger,
		clients:  make(map[string]*client.Client),
		streams:  make(map[string]*liveStream),
		watching: make(map[string]string),
		signals:  make(chan *domain.Signal, 256),
		leave:    make(chan string, 64),
		stop:     make(chan struct{}),
	}
}

func (uc *useCase) Register(userId, username string, conn *websocket.Conn) {
	c := client.New(userId, username, conn)

	uc.mu.Lock()
	if old, ok := uc.clients[userId]; ok {
		old.Close()
	}
	uc.clients[userId] = c
	uc.mu.Unlock()

	go c.WritePump()
	go c.ReadPump(
		func(s *domain.Signal) { uc.signals <- s },
		func() { uc.leave <- userId; c.Close() },
	)

	c.Send(&domain.Signal{Type: domain.SignalRegistered, To: userId})
	c.Send(&domain.Signal{Type: domain.SignalLiveList, To: userId, Payload: encodeStreams(uc.Streams())})
}

func (uc *useCase) Streams() []domain.StreamSummary {
	uc.mu.RLock()
	defer uc.mu.RUnlock()
	out := make([]domain.StreamSummary, 0, len(uc.streams))
	for _, s := range uc.streams {
		out = append(out, domain.StreamSummary{
			StreamId:    s.broadcasterId,
			Broadcaster: s.broadcaster,
			Title:       s.title,
			Viewers:     len(s.viewers),
		})
	}
	return out
}

func (uc *useCase) Run() {
	if uc.started {
		return
	}
	uc.started = true
	for {
		select {
		case s := <-uc.signals:
			uc.handle(s)
		case userId := <-uc.leave:
			uc.unregister(userId)
		case <-uc.stop:
			uc.shutdown()
			return
		}
	}
}

func (uc *useCase) Stop() {
	if !uc.started {
		return
	}
	close(uc.stop)
}

func (uc *useCase) shutdown() {
	uc.mu.Lock()
	defer uc.mu.Unlock()
	for id, c := range uc.clients {
		c.Close()
		delete(uc.clients, id)
	}
	uc.streams = map[string]*liveStream{}
	uc.watching = map[string]string{}
}

// handle dispatches one client signal to the appropriate handler.
func (uc *useCase) handle(s *domain.Signal) {
	switch s.Type {
	case domain.SignalGoLive:
		uc.handleGoLive(s)
	case domain.SignalStopLive:
		uc.handleStopLive(s.From)
	case domain.SignalWatch:
		uc.handleWatch(s)
	case domain.SignalLeave:
		uc.handleLeave(s.From)
	case domain.SignalChat:
		uc.handleChat(s)
	case domain.SignalSlide:
		uc.handleSlide(s)
	case domain.SignalRaiseHand:
		uc.handleRaiseHand(s.From)
	case domain.SignalLowerHand:
		uc.handleLowerHand(s.From)
	case domain.SignalAcceptHand:
		uc.handleAcceptHand(s.From, s.To)
	case domain.SignalRejectHand:
		uc.handleRejectHand(s.From, s.To)
	case domain.SignalRevokeSpeaker:
		uc.handleRevokeSpeaker(s.From, s.To)
	case domain.SignalOffer, domain.SignalAnswer, domain.SignalIce,
		domain.SignalBackOffer, domain.SignalBackAnswer, domain.SignalBackIce:
		if err := uc.forward(s); err != nil {
			uc.logger.Printf("live: forward %s from=%s to=%s: %v", s.Type, s.From, s.To, err)
		}
	default:
		uc.logger.Printf("live: unknown signal %q from %s", s.Type, s.From)
	}
}

func (uc *useCase) handleGoLive(s *domain.Signal) {
	title := decodeGoLive(s.Payload).Title

	uc.mu.Lock()
	c, ok := uc.clients[s.From]
	if !ok {
		uc.mu.Unlock()
		return
	}
	if _, already := uc.streams[s.From]; already {
		uc.mu.Unlock()
		c.Send(&domain.Signal{Type: domain.SignalError, To: s.From,
			Payload: encodeStreamId("already broadcasting")})
		return
	}
	uc.streams[s.From] = &liveStream{
		broadcasterId: s.From,
		broadcaster:   c.Username,
		title:         title,
		viewers:       make(map[string]struct{}),
		pendingHands:  make(map[string]domain.Hand),
	}
	uc.mu.Unlock()

	uc.broadcastList()
}

func (uc *useCase) handleStopLive(broadcasterId string) {
	uc.mu.Lock()
	stream, ok := uc.streams[broadcasterId]
	if !ok {
		uc.mu.Unlock()
		return
	}
	delete(uc.streams, broadcasterId)
	viewerIds := make([]string, 0, len(stream.viewers))
	for v := range stream.viewers {
		viewerIds = append(viewerIds, v)
		delete(uc.watching, v)
	}
	uc.mu.Unlock()

	for _, v := range viewerIds {
		uc.sendTo(v, &domain.Signal{
			Type:    domain.SignalStreamOffline,
			To:      v,
			Payload: encodeStreamId(broadcasterId),
		})
	}
	uc.broadcastList()
}

func (uc *useCase) handleWatch(s *domain.Signal) {
	streamId := decodeWatch(s.Payload).StreamId
	if streamId == "" {
		return
	}

	uc.mu.Lock()
	stream, ok := uc.streams[streamId]
	if !ok {
		uc.mu.Unlock()
		uc.sendTo(s.From, &domain.Signal{Type: domain.SignalError, To: s.From,
			Payload: encodeStreamId("stream not found")})
		return
	}
	if _, viewing := uc.watching[s.From]; viewing && uc.watching[s.From] != streamId {
		uc.removeViewerLocked(s.From)
	}
	stream.viewers[s.From] = struct{}{}
	uc.watching[s.From] = streamId
	uc.mu.Unlock()

	// Tell the broadcaster a new viewer is here so it can build a peer
	// connection and send an offer.
	uc.sendTo(streamId, &domain.Signal{
		Type: domain.SignalViewerJoined,
		From: s.From,
		To:   streamId,
	})
	// If the broadcaster already advanced past page 1, catch the new viewer up
	// so they don't have to wait for the next page turn.
	uc.mu.RLock()
	slide := uc.streams[streamId].currentSlide
	uc.mu.RUnlock()
	if slide != nil {
		uc.sendTo(s.From, &domain.Signal{
			Type:    domain.SignalSlide,
			To:      s.From,
			From:    streamId,
			Payload: encodeSlide(*slide),
		})
	}
	// Catch the new viewer up on who's currently speaking, so they can show
	// the right UI without waiting for the next change.
	uc.mu.RLock()
	speaker := uc.streams[streamId].speaker
	uc.mu.RUnlock()
	uc.sendTo(s.From, &domain.Signal{
		Type:    domain.SignalSpeakerUpdate,
		To:      s.From,
		From:    streamId,
		Payload: encodeSpeaker(speaker),
	})
	uc.broadcastList()
}

func (uc *useCase) handleLeave(viewerId string) {
	uc.mu.Lock()
	streamId, viewing := uc.watching[viewerId]
	if !viewing {
		uc.mu.Unlock()
		return
	}
	stream := uc.streams[streamId]
	wasSpeaker := stream != nil && stream.speaker != nil && stream.speaker.UserId == viewerId
	_, hadHand := func() (struct{}, bool) {
		if stream == nil {
			return struct{}{}, false
		}
		_, ok := stream.pendingHands[viewerId]
		return struct{}{}, ok
	}()
	uc.removeViewerLocked(viewerId)
	uc.mu.Unlock()

	uc.sendTo(streamId, &domain.Signal{
		Type: domain.SignalViewerLeft,
		From: viewerId,
		To:   streamId,
	})
	if wasSpeaker {
		uc.pushSpeakerUpdate(streamId, nil)
	}
	if hadHand || wasSpeaker {
		uc.pushHandsUpdate(streamId)
	}
	uc.broadcastList()
}

// handleChat fans a chat message out to every participant of the sender's
// stream — broadcaster + all viewers, including the sender so their own
// message appears in the panel.
func (uc *useCase) handleChat(s *domain.Signal) {
	text := decodeChat(s.Payload).Text
	if text == "" {
		return
	}

	uc.mu.RLock()
	sender, ok := uc.clients[s.From]
	if !ok {
		uc.mu.RUnlock()
		return
	}
	streamId := s.From
	stream, isBroadcaster := uc.streams[s.From]
	if !isBroadcaster {
		sid, viewing := uc.watching[s.From]
		if !viewing {
			uc.mu.RUnlock()
			return
		}
		streamId = sid
		stream = uc.streams[sid]
	}
	if stream == nil {
		uc.mu.RUnlock()
		return
	}
	recipients := make([]string, 0, len(stream.viewers)+1)
	recipients = append(recipients, streamId)
	for v := range stream.viewers {
		recipients = append(recipients, v)
	}
	uc.mu.RUnlock()

	payload := encodeChat(domain.ChatMessage{
		From: sender.Username,
		Text: text,
		At:   time.Now().UnixMilli(),
	})
	for _, r := range recipients {
		uc.sendTo(r, &domain.Signal{Type: domain.SignalChat, From: streamId, To: r, Payload: payload})
	}
}

// handleSlide accepts a page-change push from the broadcaster, remembers it
// so future viewers can be caught up on join, and forwards to current viewers.
func (uc *useCase) handleSlide(s *domain.Signal) {
	slide := decodeSlide(s.Payload)
	if slide.URL == "" {
		return
	}
	if slide.Page < 1 {
		slide.Page = 1
	}

	uc.mu.Lock()
	stream, isBroadcaster := uc.streams[s.From]
	if !isBroadcaster {
		uc.mu.Unlock()
		return
	}
	stream.currentSlide = &slide
	viewerIds := make([]string, 0, len(stream.viewers))
	for v := range stream.viewers {
		viewerIds = append(viewerIds, v)
	}
	uc.mu.Unlock()

	payload := encodeSlide(slide)
	for _, v := range viewerIds {
		uc.sendTo(v, &domain.Signal{Type: domain.SignalSlide, From: s.From, To: v, Payload: payload})
	}
}

// ---------- Phase 2: raise-hand / promote-to-speaker ----------

// handleRaiseHand is called by a viewer asking to be promoted to speaker.
// Adds them to the broadcaster's hand queue and pushes an updated list.
func (uc *useCase) handleRaiseHand(viewerId string) {
	uc.mu.Lock()
	streamId, viewing := uc.watching[viewerId]
	if !viewing {
		uc.mu.Unlock()
		return
	}
	stream, ok := uc.streams[streamId]
	if !ok {
		uc.mu.Unlock()
		return
	}
	c, hasClient := uc.clients[viewerId]
	if !hasClient {
		uc.mu.Unlock()
		return
	}
	// Already speaking? raising is a no-op.
	if stream.speaker != nil && stream.speaker.UserId == viewerId {
		uc.mu.Unlock()
		return
	}
	stream.pendingHands[viewerId] = domain.Hand{
		UserId:   viewerId,
		Username: c.Username,
		RaisedAt: time.Now().UnixMilli(),
	}
	uc.mu.Unlock()

	uc.pushHandsUpdate(streamId)
}

// handleLowerHand removes the viewer from the queue, or revokes them if
// they're currently speaking.
func (uc *useCase) handleLowerHand(viewerId string) {
	uc.mu.Lock()
	streamId, viewing := uc.watching[viewerId]
	if !viewing {
		uc.mu.Unlock()
		return
	}
	stream, ok := uc.streams[streamId]
	if !ok {
		uc.mu.Unlock()
		return
	}
	wasSpeaker := stream.speaker != nil && stream.speaker.UserId == viewerId
	delete(stream.pendingHands, viewerId)
	if wasSpeaker {
		stream.speaker = nil
	}
	uc.mu.Unlock()

	if wasSpeaker {
		uc.pushSpeakerUpdate(streamId, nil)
		uc.sendTo(viewerId, &domain.Signal{Type: domain.SignalSpeakerRevoked, To: viewerId})
	}
	uc.pushHandsUpdate(streamId)
}

// handleAcceptHand promotes a viewer to speaker. Auto-revokes the previous
// speaker (Phase 2 supports one at a time).
func (uc *useCase) handleAcceptHand(broadcasterId, viewerId string) {
	if viewerId == "" {
		return
	}
	uc.mu.Lock()
	stream, ok := uc.streams[broadcasterId]
	if !ok {
		uc.mu.Unlock()
		return
	}
	if _, queued := stream.pendingHands[viewerId]; !queued {
		uc.mu.Unlock()
		return
	}
	c, hasClient := uc.clients[viewerId]
	if !hasClient {
		// viewer disappeared between raising and accepting
		delete(stream.pendingHands, viewerId)
		uc.mu.Unlock()
		uc.pushHandsUpdate(broadcasterId)
		return
	}
	prevSpeakerId := ""
	if stream.speaker != nil && stream.speaker.UserId != viewerId {
		prevSpeakerId = stream.speaker.UserId
	}
	delete(stream.pendingHands, viewerId)
	stream.speaker = &domain.Speaker{
		UserId:   viewerId,
		Username: c.Username,
		Since:    time.Now().UnixMilli(),
	}
	streamId := broadcasterId
	speaker := *stream.speaker
	uc.mu.Unlock()

	if prevSpeakerId != "" {
		uc.sendTo(prevSpeakerId, &domain.Signal{Type: domain.SignalSpeakerRevoked, To: prevSpeakerId})
	}
	// Notify the new speaker that they may now publish back-channel media.
	uc.sendTo(viewerId, &domain.Signal{
		Type: domain.SignalHandAccepted,
		From: broadcasterId,
		To:   viewerId,
	})
	uc.pushSpeakerUpdate(streamId, &speaker)
	uc.pushHandsUpdate(streamId)
}

// handleRejectHand drops the viewer from the queue and tells them.
func (uc *useCase) handleRejectHand(broadcasterId, viewerId string) {
	if viewerId == "" {
		return
	}
	uc.mu.Lock()
	stream, ok := uc.streams[broadcasterId]
	if !ok {
		uc.mu.Unlock()
		return
	}
	if _, queued := stream.pendingHands[viewerId]; !queued {
		uc.mu.Unlock()
		return
	}
	delete(stream.pendingHands, viewerId)
	uc.mu.Unlock()

	uc.sendTo(viewerId, &domain.Signal{
		Type: domain.SignalHandRejected,
		From: broadcasterId,
		To:   viewerId,
	})
	uc.pushHandsUpdate(broadcasterId)
}

// handleRevokeSpeaker forcibly demotes the current speaker.
func (uc *useCase) handleRevokeSpeaker(broadcasterId, viewerId string) {
	uc.mu.Lock()
	stream, ok := uc.streams[broadcasterId]
	if !ok {
		uc.mu.Unlock()
		return
	}
	if stream.speaker == nil {
		uc.mu.Unlock()
		return
	}
	if viewerId != "" && stream.speaker.UserId != viewerId {
		uc.mu.Unlock()
		return
	}
	demotedId := stream.speaker.UserId
	stream.speaker = nil
	uc.mu.Unlock()

	uc.sendTo(demotedId, &domain.Signal{Type: domain.SignalSpeakerRevoked, To: demotedId})
	uc.pushSpeakerUpdate(broadcasterId, nil)
}

// pushHandsUpdate sends the current queue to the stream's broadcaster.
func (uc *useCase) pushHandsUpdate(streamId string) {
	uc.mu.RLock()
	stream, ok := uc.streams[streamId]
	if !ok {
		uc.mu.RUnlock()
		return
	}
	hands := make([]domain.Hand, 0, len(stream.pendingHands))
	for _, h := range stream.pendingHands {
		hands = append(hands, h)
	}
	uc.mu.RUnlock()
	sort.Slice(hands, func(i, j int) bool { return hands[i].RaisedAt < hands[j].RaisedAt })
	uc.sendTo(streamId, &domain.Signal{
		Type:    domain.SignalHandsUpdate,
		To:      streamId,
		Payload: encodeHands(hands),
	})
}

// pushSpeakerUpdate broadcasts the current speaker (or nil) to everyone in
// the stream so all clients can show the right indicator.
func (uc *useCase) pushSpeakerUpdate(streamId string, sp *domain.Speaker) {
	payload := encodeSpeaker(sp)
	uc.mu.RLock()
	stream, ok := uc.streams[streamId]
	if !ok {
		uc.mu.RUnlock()
		return
	}
	recipients := make([]string, 0, len(stream.viewers)+1)
	recipients = append(recipients, streamId)
	for v := range stream.viewers {
		recipients = append(recipients, v)
	}
	uc.mu.RUnlock()
	for _, r := range recipients {
		uc.sendTo(r, &domain.Signal{
			Type:    domain.SignalSpeakerUpdate,
			From:    streamId,
			To:      r,
			Payload: payload,
		})
	}
}

// removeViewerLocked drops viewerId from whatever stream they were watching,
// also clearing any pending hand-raise or speaker state they held. Caller
// must hold uc.mu. The boolean tells the caller they need to fan out a
// speaker-update afterwards (which acquires the lock again).
func (uc *useCase) removeViewerLocked(viewerId string) {
	streamId, viewing := uc.watching[viewerId]
	if !viewing {
		return
	}
	delete(uc.watching, viewerId)
	stream, ok := uc.streams[streamId]
	if !ok {
		return
	}
	delete(stream.viewers, viewerId)
	delete(stream.pendingHands, viewerId)
	if stream.speaker != nil && stream.speaker.UserId == viewerId {
		stream.speaker = nil
	}
}

func (uc *useCase) forward(s *domain.Signal) error {
	if s.To == "" {
		return errors.New("missing 'to'")
	}
	uc.mu.RLock()
	target, ok := uc.clients[s.To]
	uc.mu.RUnlock()
	if !ok {
		return errors.New("recipient offline")
	}
	target.Send(s)
	return nil
}

func (uc *useCase) sendTo(userId string, s *domain.Signal) {
	uc.mu.RLock()
	c, ok := uc.clients[userId]
	uc.mu.RUnlock()
	if ok {
		c.Send(s)
	}
}

func (uc *useCase) broadcastList() {
	streams := uc.Streams()
	payload := encodeStreams(streams)
	uc.mu.RLock()
	defer uc.mu.RUnlock()
	for _, c := range uc.clients {
		c.Send(&domain.Signal{Type: domain.SignalLiveList, To: c.UserId, Payload: payload})
	}
}

// unregister cleans up a disconnected client: tears down their stream if they
// were broadcasting, removes them from any stream they were watching.
func (uc *useCase) unregister(userId string) {
	uc.mu.Lock()
	c, ok := uc.clients[userId]
	if ok {
		delete(uc.clients, userId)
	}
	wasBroadcasting := false
	if _, broadcasting := uc.streams[userId]; broadcasting {
		wasBroadcasting = true
	}
	streamId, viewing := uc.watching[userId]
	wasSpeaker := false
	hadHand := false
	if viewing {
		if stream, ok := uc.streams[streamId]; ok {
			if stream.speaker != nil && stream.speaker.UserId == userId {
				wasSpeaker = true
			}
			if _, queued := stream.pendingHands[userId]; queued {
				hadHand = true
			}
		}
		uc.removeViewerLocked(userId)
	}
	uc.mu.Unlock()

	if ok {
		c.Close()
	}
	if wasBroadcasting {
		uc.handleStopLive(userId)
		return
	}
	if viewing {
		uc.sendTo(streamId, &domain.Signal{
			Type: domain.SignalViewerLeft,
			From: userId,
			To:   streamId,
		})
		if wasSpeaker {
			uc.pushSpeakerUpdate(streamId, nil)
		}
		if hadHand || wasSpeaker {
			uc.pushHandsUpdate(streamId)
		}
		uc.broadcastList()
	}
}
