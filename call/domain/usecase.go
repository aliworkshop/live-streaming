package domain

import "github.com/gorilla/websocket"

type CallUc interface {
	Register(userId, username string, conn *websocket.Conn)
	Unregister(userId string)
	Forward(s *Signal) error
	Peers() []Peer
	Run()
	Stop()
}
