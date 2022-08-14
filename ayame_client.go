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

	ws         *websocket.Conn
	closed     atomic.Bool
	ready      atomic.Bool
	done       chan struct{}
	candidates []*ICECandidate
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
	msgCh := make(chan *SignalingMessage, 32)
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
			select {
			case <-done:
				return
			default:
			}
			switch msg.Type {
			case "ping":
				err := ws.WriteJSON(&EmptyMessage{Type: "pong"})
				if err != nil {
					conn.LastError = err
					return
				}
			case "pong":
			case "bye":
				return
			case "offer", "answer", "candidate":
				select {
				case <-done:
					return
				case msgCh <- &msg:
				case <-time.After(200 * time.Millisecond):
					log.Println("msg timeout", msg.Type)
				}
			default:
				log.Println("unknown message type:", msg.Type)
			}
			if msg.Type == "answer" {
				conn.ready.Store(true)
				for _, cand := range conn.candidates {
					ws.WriteJSON(&SignalingMessage{Type: "candidate", ICE: cand})
				}
			}
		}
	}()

	return conn, nil
}

func (c *AyameClient) Answer(sdp string) error {
	return c.ws.WriteJSON(&SignalingMessage{Type: "answer", SDP: sdp})
}

func (c *AyameClient) Offer(sdp string) error {
	return c.ws.WriteJSON(&SignalingMessage{Type: "offer", SDP: sdp})
}

func (c *AyameClient) Candidate(candidate string, id *string, index *uint16) error {
	cand := &ICECandidate{Candidate: candidate, SdpMid: id, SdpMLineIndex: index}
	if !c.ready.Load() {
		c.candidates = append(c.candidates, cand)
		return nil
	}
	return c.ws.WriteJSON(&SignalingMessage{Type: "candidate", ICE: cand})

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
