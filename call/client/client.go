package client

import (
	"sync"
	"time"

	"github.com/aliworkshop/live-streaming/call/domain"
	"github.com/gorilla/websocket"
)

const (
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = 30 * time.Second
)

type Client struct {
	UserId   string
	Username string

	conn   *websocket.Conn
	send   chan *domain.Signal
	closed bool
	mu     sync.Mutex
}

func New(userId, username string, conn *websocket.Conn) *Client {
	return &Client{
		UserId:   userId,
		Username: username,
		conn:     conn,
		send:     make(chan *domain.Signal, 32),
	}
}

func (c *Client) Send(s *domain.Signal) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	select {
	case c.send <- s:
	default:
		// drop on overflow rather than block the hub
	}
}

func (c *Client) Close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	close(c.send)
	c.mu.Unlock()
	_ = c.conn.Close()
}

func (c *Client) ReadPump(onSignal func(*domain.Signal), onClose func()) {
	defer onClose()
	c.conn.SetReadLimit(1 << 20)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	for {
		var s domain.Signal
		if err := c.conn.ReadJSON(&s); err != nil {
			return
		}
		s.From = c.UserId
		onSignal(&s)
	}
}

func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()
	for {
		select {
		case s, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteJSON(s); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
