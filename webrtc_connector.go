package main

import (
	"context"
	"log"

	"github.com/pion/webrtc/v3"
)

type RTCConn struct {
	ayameConn *AyameConn
	PC        *webrtc.PeerConnection
}

func NewRTCConn(signalingUrl, roomID, signalingKey string) (*RTCConn, error) {
	conn, err := Dial(signalingUrl, roomID, signalingKey)
	if err != nil {
		return nil, err
	}

	rtcConfig := webrtc.Configuration{}
	for _, iceServer := range conn.AuthResult.IceServers {
		rtcConfig.ICEServers = append(rtcConfig.ICEServers, webrtc.ICEServer{
			URLs:       iceServer.URLs,
			Username:   iceServer.Username,
			Credential: iceServer.Credential,
		})
	}

	peerConnection, err := webrtc.NewPeerConnection(rtcConfig)
	if err != nil {
		conn.Close()
		return nil, err
	}
	return &RTCConn{ayameConn: conn, PC: peerConnection}, nil
}

func (c *RTCConn) Start() {
	c.PC.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		log.Printf("Peer Connection State has changed: %s\n", s.String())
		if s == webrtc.PeerConnectionStateFailed || s == webrtc.PeerConnectionStateDisconnected || s == webrtc.PeerConnectionStateClosed {
			c.ayameConn.Close()
		}
	})

	// Trickle ICE
	c.PC.OnICECandidate(func(ic *webrtc.ICECandidate) {
		if ic == nil {
			return
		}
		cand := ic.ToJSON()
		c.ayameConn.Candidate(cand.Candidate, cand.SDPMid, cand.SDPMLineIndex)
	})

	if c.ayameConn.AuthResult.IsExistClient {
		offer, _ := c.PC.CreateOffer(nil)
		if err := c.PC.SetLocalDescription(offer); err != nil {
			log.Fatal(err)
		}
		c.ayameConn.Offer(offer.SDP)
	}
	go func() {
		for msg := range c.ayameConn.Msg {
			switch msg.Type {
			case "candidate":
				cand := webrtc.ICECandidateInit{Candidate: msg.ICE.Candidate, SDPMid: msg.ICE.SdpMid, SDPMLineIndex: msg.ICE.SdpMLineIndex}
				if err := c.PC.AddICECandidate(cand); err != nil {
					log.Fatal(err)
				}
			case "offer":
				desc := webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: msg.SDP}
				if err := c.PC.SetRemoteDescription(desc); err != nil {
					log.Fatal(err)
				}

				answer, err := c.PC.CreateAnswer(nil)
				if err != nil {
					log.Fatal(err)
				}
				if err := c.PC.SetLocalDescription(answer); err != nil {
					log.Fatal(err)
				}
				c.ayameConn.Answer(answer.SDP)
			case "answer":
				desc := webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: msg.SDP}
				if err := c.PC.SetRemoteDescription(desc); err != nil {
					log.Println(err)
				}
			default:
				log.Println("Unknown message:", msg.Type)
			}
		}
	}()
}

func (c *RTCConn) Close() error {
	c.ayameConn.Close()
	return c.PC.Close()
}

func (c *RTCConn) Wait(ctx context.Context) error {
	select {
	case <-c.ayameConn.Done():
		return c.ayameConn.LastError
	case <-ctx.Done():
		return ctx.Err()
	}
}
