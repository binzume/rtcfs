package main

import (
	"fmt"
	"log"
	"sync"
	"sync/atomic"

	"github.com/gorilla/websocket"
)

type AyameConn struct {
	Msg        <-chan *SignalingMessage
	AuthResult *AuthResultMessage
	LastError  error

	soc        JsonSocket
	closed     atomic.Bool
	ready      atomic.Bool
	done       chan struct{}
	candidates []*ICECandidate

	sendLock sync.Mutex
}

type JsonSocket interface {
	WriteJSON(v any) error
	ReadJSON(v any) error
	Close() error
}

func Dial(signalingUrl, roomID, signalingKey string) (*AyameConn, error) {
	ws, _, err := websocket.DefaultDialer.Dial(signalingUrl, nil)
	if err != nil {
		return nil, err
	}
	return StartClient(ws, roomID, signalingKey)
}

func StartClient(soc JsonSocket, roomID, signalingKey string) (*AyameConn, error) {
	done := make(chan struct{})
	msgCh := make(chan *SignalingMessage, 32)
	conn := &AyameConn{soc: soc, done: done, Msg: msgCh}
	if err := conn.handshake(roomID, signalingKey); err != nil {
		soc.Close()
		return nil, err
	}
	go conn.recvLoop(msgCh)
	return conn, nil
}

func (c *AyameConn) handshake(roomID, signalingKey string) error {
	err := c.soc.WriteJSON(&RegisterMessage{
		Type:         "register",
		RoomID:       roomID,
		SignalingKey: signalingKey,
	})
	if err != nil {
		return err
	}
	var authResult AuthResultMessage
	err = c.soc.ReadJSON(&authResult)
	if err != nil {
		return err
	}
	c.AuthResult = &authResult
	if authResult.Type != "accept" {
		return fmt.Errorf("Auth error: %s", authResult.Reason)
	}
	return nil
}

func (c *AyameConn) recvLoop(msgCh chan<- *SignalingMessage) {
	defer c.Close()
	defer close(msgCh)
	for {
		var msg SignalingMessage
		err := c.soc.ReadJSON(&msg)
		if err != nil {
			c.LastError = err
			return
		}
		select {
		case <-c.done:
			return
		default:
		}
		switch msg.Type {
		case "ping":
			err := c.soc.WriteJSON(&EmptyMessage{Type: "pong"})
			if err != nil {
				c.LastError = err
				return
			}
		case "pong":
		case "bye":
			return
		case "offer", "answer", "candidate":
			select {
			case <-c.done:
				return
			case msgCh <- &msg:
			}
		default:
			log.Println("unknown message type:", msg.Type)
		}
		if msg.Type == "answer" || msg.Type == "offer" {
			c.ready.Store(true)
			for _, cand := range c.candidates {
				c.send(&SignalingMessage{Type: "candidate", ICE: cand})
			}
		}
	}
}

func (c *AyameConn) send(msg *SignalingMessage) error {
	c.sendLock.Lock()
	defer c.sendLock.Unlock()
	return c.soc.WriteJSON(msg)
}

func (c *AyameConn) Answer(sdp string) error {
	return c.send(&SignalingMessage{Type: "answer", SDP: sdp})
}

func (c *AyameConn) Offer(sdp string) error {
	return c.send(&SignalingMessage{Type: "offer", SDP: sdp})
}

func (c *AyameConn) Candidate(candidate string, id *string, index *uint16) error {
	cand := &ICECandidate{Candidate: candidate, SdpMid: id, SdpMLineIndex: index}
	if !c.ready.Load() {
		c.candidates = append(c.candidates, cand)
		return nil
	}
	return c.send(&SignalingMessage{Type: "candidate", ICE: cand})
}

func (c *AyameConn) Close() {
	if !c.closed.CompareAndSwap(false, true) {
		return
	}
	close(c.done)
	c.soc.Close()
}

func (c *AyameConn) Done() <-chan struct{} {
	return c.done
}
