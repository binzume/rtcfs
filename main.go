package main

import (
	"errors"
	"flag"
	"log"
	"os"

	"github.com/BurntSushi/toml"
	"github.com/pion/webrtc/v3"
)

type Config struct {
	SignalingUrl string
	SignalingKey string
	RoomIdPrefix string

	RoomName  string
	LocalPath string

	ThumbnailCacheDir string
	FFmpegPath        string
}

const defaultSignalingUrl = "wss://ayame-labo.shiguredo.app/signaling"
const defaultSignalingKey = "VV69g7Ngx-vNwNknLhxJPHs9FpRWWNWeUzJ9FUyylkD_yc_F"
const defaultRoomIdPrefix = "binzume@rdp-room-"

func InitFileHandler(d *webrtc.DataChannel, handler *FileHandler) {
	d.OnMessage(func(msg webrtc.DataChannelMessage) {
		res := handler.HandleMessage(msg.Data, msg.IsString)
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

func loadConfig(confPath string) *Config {
	var config Config
	config.SignalingUrl = defaultSignalingUrl
	config.SignalingKey = defaultSignalingKey
	config.RoomIdPrefix = defaultRoomIdPrefix
	config.ThumbnailCacheDir = "cache"

	_, err := toml.DecodeFile(confPath, &config)
	if errors.Is(err, os.ErrNotExist) {
		log.Printf("WARN: %s not found. use default settings.\n", confPath)
	} else if err != nil {
		log.Print("Failed to load ", confPath, err)
	}
	return &config
}

func Start(config *Config) error {
	var fileHander = &FileHandler{Fs: os.DirFS(config.LocalPath)}

	conn, err := Connect(config.SignalingUrl, config.RoomIdPrefix+config.RoomName, config.SignalingKey)
	if err != nil {
		return err
	}
	defer conn.Close()

	rtcConfig := webrtc.Configuration{}
	for _, iceServer := range conn.AuthResult.IceServers {
		rtcConfig.ICEServers = append(rtcConfig.ICEServers, webrtc.ICEServer{
			URLs:       iceServer.URLs,
			Username:   *iceServer.UserName,
			Credential: *iceServer.Credential,
		})
	}

	peerConnection, err := webrtc.NewPeerConnection(rtcConfig)
	if err != nil {
		return err
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
			InitFileHandler(d, fileHander)
		}
	})

	go func() {
		for msg := range conn.Msg {
			switch msg.Type {
			case "candidate":
				cand := webrtc.ICECandidateInit{Candidate: msg.ICE.Candidate, SDPMid: msg.ICE.SdpMid}
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
				desc := webrtc.SessionDescription{Type: webrtc.SDPTypePranswer, SDP: msg.SDP}
				if err := peerConnection.SetRemoteDescription(desc); err != nil {
					log.Println(err)
				}
			default:
				log.Println("Unknown message:", msg.Type)
			}
		}
	}()

	if conn.AuthResult.IsExistClient {
		// Trickle ICE
		peerConnection.OnICECandidate(func(c *webrtc.ICECandidate) {
			if c == nil {
				return
			}
			cand := c.ToJSON()
			conn.Candidate(cand.Candidate, cand.SDPMid, cand.SDPMLineIndex)
		})

		// TODO
		peerConnection.CreateDataChannel("fileServer", nil)

		offer, _ := peerConnection.CreateOffer(nil)
		if err := peerConnection.SetLocalDescription(offer); err != nil {
			log.Fatal(err)
		}
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

	return conn.LastError
}

func main() {
	confPath := flag.String("conf", "config.toml", "conf path")
	roomName := flag.String("room", "", "Ayame room name")
	localPath := flag.String("path", ".", "local path to share")
	flag.Parse()

	config := loadConfig(*confPath)
	if *localPath != "" {
		config.LocalPath = *localPath
	}
	if *roomName != "" {
		config.RoomName = *roomName
	}

	if config.ThumbnailCacheDir != "" {
		DefaultThumbnailer.Register(NewImageThumbnailer(config.ThumbnailCacheDir))
		if config.FFmpegPath != "" {
			DefaultThumbnailer.Register(NewVideoThumbnailer(config.ThumbnailCacheDir, config.FFmpegPath))
		}
	}

	for {
		err := Start(config)
		if err != nil {
			log.Println(err)
			return
		}
	}
}
