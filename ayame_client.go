package main

import (
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

type AyameClient struct {
	Msg        <-chan *SignalingMessage
	AuthResult *AuthResultMessage
	LastError  error

	ws     *websocket.Conn
	closed atomic.Bool
	done   chan struct{}
}

func Connect(wsurl string, roomID string, signalingKey string) (*AyameClient, error) {
	ws, _, err := websocket.DefaultDialer.Dial(wsurl, nil)
	if err != nil {
		return nil, err
	}
	return ConnectWithWs(ws, roomID, signalingKey)
}

func ConnectWithWs(ws *websocket.Conn, roomID string, signalingKey string) (*AyameClient, error) {
	err := ws.WriteJSON(&RegisterMessage{
		Type:         "register",
		RoomID:       roomID,
		SignalingKey: signalingKey,
	})
	if err != nil {
		return nil, err
	}
	var authResult AuthResultMessage
	err = ws.ReadJSON(&authResult)
	if err != nil {
		return nil, err
	}
	if authResult.Type != "accept" {
		return nil, fmt.Errorf("Auth error: %v", authResult)
	}

	done := make(chan struct{})
	msgCh := make(chan *SignalingMessage, 16)
	conn := &AyameClient{ws: ws, done: done, Msg: msgCh, AuthResult: &authResult}

	go func() {
		defer conn.Close()
		for {
			var msg SignalingMessage
			err := ws.ReadJSON(&msg)
			if err != nil {
				conn.LastError = err
				return
			}
			if msg.Type == "ping" {
				err := ws.WriteJSON(&PongMessage{Type: "pong"})
				if err != nil {
					conn.LastError = err
					return
				}
				continue
			} else if msg.Type == "bye" {
				return
			}
			select {
			case <-done:
				return
			case msgCh <- &msg:
			case <-time.After(10 * time.Millisecond):
				log.Println("timeout", msg.Type)
			}
		}
	}()

	return conn, nil
}

func (c *AyameClient) Answer(sdp string) error {
	return c.ws.WriteJSON(&SignalingMessage{
		Type: "answer",
		SDP:  sdp,
	})
}

func (c *AyameClient) Offer(sdp string) error {
	return c.ws.WriteJSON(&SignalingMessage{
		Type: "offer",
		SDP:  sdp,
	})
}

func (c *AyameClient) Done() <-chan struct{} {
	return c.done
}

func (c *AyameClient) Close() {
	if !c.closed.CompareAndSwap(false, true) {
		return
	}
	close(c.done)
	c.ws.Close()
	c.ws = nil

}

// WSUpgrader for upgrading http request in handle request
var WSUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}
