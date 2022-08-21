package rtcfs

import (
	"context"
	"log"

	"github.com/binzume/webrtcfs/ayame"
	"github.com/pion/webrtc/v3"
)

type RTCConn struct {
	ayameConn *ayame.AyameConn
	PC        *webrtc.PeerConnection
}

type DataChannelHandler interface {
	Label() string
	OnOpen(*webrtc.DataChannel)
	OnClose(*webrtc.DataChannel)
	OnMessage(*webrtc.DataChannel, webrtc.DataChannelMessage)
}

type DataChannelCallback struct {
	Name          string
	OnOpenFunc    func(*webrtc.DataChannel)
	OnCloseFunc   func(*webrtc.DataChannel)
	OnMessageFunc func(*webrtc.DataChannel, webrtc.DataChannelMessage)
}

func (d *DataChannelCallback) Label() string {
	return d.Name
}

func (d *DataChannelCallback) OnOpen(ch *webrtc.DataChannel) {
	if d.OnOpenFunc != nil {
		d.OnOpenFunc(ch)
	}
}

func (d *DataChannelCallback) OnClose(ch *webrtc.DataChannel) {
	if d.OnCloseFunc != nil {
		d.OnCloseFunc(ch)
	}
}

func (d *DataChannelCallback) OnMessage(ch *webrtc.DataChannel, msg webrtc.DataChannelMessage) {
	if d.OnMessageFunc != nil {
		d.OnMessageFunc(ch, msg)
	}
}

func initDataChannelHandler(d *webrtc.DataChannel, handler DataChannelHandler) {
	d.OnOpen(func() { handler.OnOpen(d) })
	d.OnMessage(func(msg webrtc.DataChannelMessage) { handler.OnMessage(d, msg) })
	d.OnClose(func() { handler.OnClose(d) })
}

func NewRTCConn(signalingUrl, roomID, signalingKey string) (*RTCConn, error) {
	conn, err := ayame.Dial(signalingUrl, roomID, signalingKey)
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

func (c *RTCConn) IsExistRoom() bool {
	return c.ayameConn.AuthResult.IsExistClient
}

// AddTrack/CreateDataChannel shoudl be done before Start()
func (c *RTCConn) Start(dataChannles []DataChannelHandler) {
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

	// Data channels
	if len(dataChannles) > 0 {
		c.PC.OnDataChannel(func(d *webrtc.DataChannel) {
			for _, c := range dataChannles {
				if c.Label() == d.Label() {
					initDataChannelHandler(d, c)
				}
			}
		})
		if c.ayameConn.AuthResult.IsExistClient {
			for _, d := range dataChannles {
				dc, _ := c.PC.CreateDataChannel(d.Label(), nil)
				initDataChannelHandler(dc, d)
			}
		}
	}

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
