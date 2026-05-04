package usecase

import (
	"errors"
	"log"
	"sync"

	"github.com/aliworkshop/live-streaming/live/client"
	"github.com/aliworkshop/live-streaming/live/domain"
	"github.com/gorilla/websocket"
)

type liveStream struct {
	broadcasterId string
	broadcaster   string
	title         string
	viewers       map[string]struct{}
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
	case domain.SignalOffer, domain.SignalAnswer, domain.SignalIce:
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
	uc.broadcastList()
}

func (uc *useCase) handleLeave(viewerId string) {
	uc.mu.Lock()
	streamId, viewing := uc.watching[viewerId]
	if !viewing {
		uc.mu.Unlock()
		return
	}
	uc.removeViewerLocked(viewerId)
	uc.mu.Unlock()

	uc.sendTo(streamId, &domain.Signal{
		Type: domain.SignalViewerLeft,
		From: viewerId,
		To:   streamId,
	})
	uc.broadcastList()
}

// removeViewerLocked drops viewerId from whatever stream they were watching.
// Caller must hold uc.mu.
func (uc *useCase) removeViewerLocked(viewerId string) {
	streamId, viewing := uc.watching[viewerId]
	if !viewing {
		return
	}
	delete(uc.watching, viewerId)
	if stream, ok := uc.streams[streamId]; ok {
		delete(stream.viewers, viewerId)
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
	if viewing {
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
		uc.broadcastList()
	}
}
