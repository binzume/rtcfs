package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/pion/webrtc/v3"
)

const signalingUrl = "wss://ayame-labo.shiguredo.app/signaling"
const signalingKey = ""
const roomIdPrefix = "binzume-rdp-room-"
const roomName = ""

var fileHander = &FileHandler{Fs: os.DirFS("..")}

func HandleMessage(msg webrtc.DataChannelMessage) *FileOperationResult {
	var result *FileOperationResult
	if msg.IsString {
		var op FileOperation
		_ = json.Unmarshal(msg.Data, &op)
		data, err := fileHander.HanldeFileOp(&op)
		if err != nil {
			result = &FileOperationResult{RID: op.RID, Data: data, Error: fmt.Sprint(err)}
		} else if data != nil {
			result = &FileOperationResult{RID: op.RID, Data: data}
		}
	} else {
		log.Println("TODO: binary message")
	}
	return result
}

func InitFileHandler(d *webrtc.DataChannel) {
	d.OnMessage(func(msg webrtc.DataChannelMessage) {
		res := HandleMessage(msg)
		if res != nil {
			b, _ := res.ToBytes()
			if res.IsBinary() {
				d.Send(b)
			} else {
				d.SendText(string(b))
			}
		}
	})
}

func main() {

	conn, err := Connect(signalingUrl, roomIdPrefix+roomName, signalingKey)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	config := webrtc.Configuration{}
	for _, iceServer := range conn.AuthResult.IceServers {
		log.Println(iceServer.URLs, *iceServer.Credential, *iceServer.UserName)
		config.ICEServers = append(config.ICEServers, webrtc.ICEServer{
			URLs:       iceServer.URLs,
			Username:   *iceServer.UserName,
			Credential: *iceServer.Credential,
		})
	}

	log.Println("start")
	peerConnection, err := webrtc.NewPeerConnection(config)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if cErr := peerConnection.Close(); cErr != nil {
			log.Printf("cannot close peerConnection: %v\n", cErr)
		}
	}()

	peerConnection.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		log.Printf("Peer Connection State has changed: %s\n", s.String())

		if s == webrtc.PeerConnectionStateFailed {
			log.Fatal("Peer Connection has gone to failed exiting")
		}
	})

	peerConnection.OnDataChannel(func(d *webrtc.DataChannel) {
		log.Printf("New DataChannel %s %d\n", d.Label(), d.ID())
		if d.Label() == "fileServer" {
			log.Printf("Start file server!")
			InitFileHandler(d)
		}
	})
	log.Println("ok")

	go func() {
		for msg := range conn.Msg {
			switch msg.Type {
			case "candidate":
				cand := webrtc.ICECandidateInit{Candidate: msg.ICE.Candidate}
				if err := peerConnection.AddICECandidate(cand); err != nil {
					log.Fatal(err)
				}
			case "offer":
				desc := webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: msg.SDP}
				if err := peerConnection.SetRemoteDescription(desc); err != nil {
					log.Fatal(err)
				}

				answer, err := peerConnection.CreateAnswer(nil)
				if err != nil {
					log.Fatal(err)
				}
				if err := peerConnection.SetLocalDescription(answer); err != nil {
					log.Fatal(err)
				}
				conn.Answer(answer.SDP)
			case "answer":
				desc := webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: msg.SDP}
				if err := peerConnection.SetRemoteDescription(desc); err != nil {
					log.Fatal(err)
				}
			default:
				log.Println("Unknown message:", msg.Type)
			}
		}
	}()

	if conn.AuthResult.IsExistClient {
		offer, _ := peerConnection.CreateOffer(nil)
		peerConnection.SetLocalDescription(offer)
		conn.Offer(offer.SDP)
	}

	gatherComplete := webrtc.GatheringCompletePromise(peerConnection)

	log.Println("wait for gatherComplete")

	<-gatherComplete

	log.Println("gatherComplete")

	<-conn.Done()
	if conn.LastError != nil {
		log.Println(conn.LastError)
	}
}
