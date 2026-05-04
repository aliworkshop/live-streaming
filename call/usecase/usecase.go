package usecase

import (
	"errors"
	"log"
	"sync"

	"github.com/aliworkshop/live-streaming/call/client"
	"github.com/aliworkshop/live-streaming/call/domain"
	"github.com/gorilla/websocket"
)

type useCase struct {
	logger *log.Logger

	mu      sync.RWMutex
	clients map[string]*client.Client

	signals chan *domain.Signal
	leave   chan string
	stop    chan struct{}
	started bool
}

func New(logger *log.Logger) domain.CallUc {
	return &useCase{
		logger:  logger,
		clients: make(map[string]*client.Client),
		signals: make(chan *domain.Signal, 256),
		leave:   make(chan string, 64),
		stop:    make(chan struct{}),
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
	uc.broadcastPeers()
}

func (uc *useCase) Unregister(userId string) {
	uc.mu.Lock()
	c, ok := uc.clients[userId]
	if ok {
		delete(uc.clients, userId)
	}
	uc.mu.Unlock()
	if ok {
		c.Close()
	}
	uc.broadcastPeers()
}

func (uc *useCase) Forward(s *domain.Signal) error {
	if s.To == "" {
		return errors.New("missing 'to'")
	}
	uc.mu.RLock()
	target, ok := uc.clients[s.To]
	uc.mu.RUnlock()
	if !ok {
		uc.mu.RLock()
		from, fok := uc.clients[s.From]
		uc.mu.RUnlock()
		if fok {
			from.Send(&domain.Signal{Type: domain.SignalPeerOffline, To: s.From, From: s.To})
		}
		return errors.New("peer offline")
	}
	target.Send(s)
	return nil
}

func (uc *useCase) Peers() []domain.Peer {
	uc.mu.RLock()
	defer uc.mu.RUnlock()
	out := make([]domain.Peer, 0, len(uc.clients))
	for _, c := range uc.clients {
		out = append(out, domain.Peer{UserId: c.UserId, Username: c.Username})
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
			if err := uc.Forward(s); err != nil {
				uc.logger.Printf("call: forward error from=%s to=%s type=%s: %v",
					s.From, s.To, s.Type, err)
			}
		case userId := <-uc.leave:
			uc.Unregister(userId)
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
}

func (uc *useCase) broadcastPeers() {
	peers := uc.Peers()
	uc.mu.RLock()
	defer uc.mu.RUnlock()
	for _, c := range uc.clients {
		filtered := make([]domain.Peer, 0, len(peers))
		for _, p := range peers {
			if p.UserId != c.UserId {
				filtered = append(filtered, p)
			}
		}
		payload, _ := encodePeers(filtered)
		c.Send(&domain.Signal{Type: domain.SignalPeers, To: c.UserId, Payload: payload})
	}
}
