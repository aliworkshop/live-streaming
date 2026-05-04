package domain

import "github.com/gorilla/websocket"

type LiveUc interface {
	Register(userId, username string, conn *websocket.Conn)
	Streams() []StreamSummary
	Run()
	Stop()
}
