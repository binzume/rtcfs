package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"math/big"
	"os"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/pion/webrtc/v3"
)

type Config struct {
	SignalingUrl        string
	SignalingKey        string
	RoomIdPrefix        string
	PairingRoomIdPrefix string
	PairingTimeoutSec   int

	RoomName  string
	AuthToken string
	LocalPath string

	ThumbnailCacheDir string
	FFmpegPath        string
}

func DefaultConfig() *Config {
	var config Config
	config.SignalingUrl = "wss://ayame-labo.shiguredo.app/signaling"
	config.SignalingKey = "VV69g7Ngx-vNwNknLhxJPHs9FpRWWNWeUzJ9FUyylkD_yc_F"
	config.RoomIdPrefix = "binzume@rdp-room-"
	config.PairingRoomIdPrefix = "binzume@rdp-pin-"
	config.PairingTimeoutSec = 600
	config.ThumbnailCacheDir = "cache"
	return &config
}

func loadConfig(confPath string) *Config {
	config := DefaultConfig()

	_, err := toml.DecodeFile(confPath, config)
	if errors.Is(err, os.ErrNotExist) {
		log.Printf("WARN: %s not found. use default settings.\n", confPath)
	} else if err != nil {
		log.Fatal("Failed to load ", confPath, err)
	}
	return config
}

func PublishFiles(ctx context.Context, config *Config) error {
	roomID := config.RoomIdPrefix + config.RoomName + ".1"
	log.Println("waiting for connect: ", roomID)

	authorized := config.AuthToken == ""

	rtcConn, err := NewRTCConn(config.SignalingUrl, roomID, config.SignalingKey)
	if err != nil {
		return err
	}
	defer func() {
		if err := rtcConn.Close(); err != nil {
			log.Printf("cannot close peerConnection: %v\n", err)
		}
	}()

	fileHander := NewFSServer(os.DirFS(config.LocalPath), 8)

	dataChannels := []DataChannelHandler{&DataChannelCallback{
		Name: "fileServer",
		OnMessageFunc: func(d *webrtc.DataChannel, msg webrtc.DataChannelMessage) {
			if !authorized {
				// TODO: error response
				return
			}
			fileHander.HandleMessage(ctx, msg.Data, msg.IsString, func(res *FileOperationResult) error {
				if res.IsJSON() {
					return d.SendText(string(res.ToBytes()))
				} else {
					return d.Send(res.ToBytes())
				}
			})
		},
	}, &DataChannelCallback{
		Name: "controlEvent",
		OnMessageFunc: func(d *webrtc.DataChannel, msg webrtc.DataChannelMessage) {
			var auth struct {
				Type  string `json:"type"`
				Token string `json:"token"`
			}
			_ = json.Unmarshal(msg.Data, &auth)
			if auth.Type == "auth" {
				authorized = auth.Token == config.AuthToken
				j, _ := json.Marshal(map[string]interface{}{
					"type":   "authResult",
					"result": authorized,
				})
				d.SendText(string(j))
			}
		},
	}}

	rtcConn.Start(dataChannels)
	return rtcConn.Wait(ctx)
}

func TraverseForTest(ctx context.Context, config *Config) error {
	roomID := config.RoomIdPrefix + config.RoomName + ".1"
	log.Println("waiting for connect: ", roomID)
	var client *FSClient
	authorized := config.AuthToken == ""

	rtcConn, err := NewRTCConn(config.SignalingUrl, roomID, config.SignalingKey)
	if err != nil {
		return err
	}
	defer func() {
		if err := rtcConn.Close(); err != nil {
			log.Printf("cannot close peerConnection: %v\n", err)
		}
	}()

	var wg sync.WaitGroup
	wg.Add(1)
	if config.AuthToken != "" {
		wg.Add(1)
	}

	dataChannels := []DataChannelHandler{&DataChannelCallback{
		Name: "fileServer",
		OnOpenFunc: func(dc *webrtc.DataChannel) {
			client = NewFSClient(func(req *FileOperationRequest) error {
				return dc.SendText(string(req.ToBytes()))
			})
			wg.Done()
		},
		OnMessageFunc: func(d *webrtc.DataChannel, msg webrtc.DataChannelMessage) {
			if client != nil {
				client.HandleMessage(msg.Data, msg.IsString)
			}
		},
	}, &DataChannelCallback{
		Name: "controlEvent",
		OnOpenFunc: func(d *webrtc.DataChannel) {
			if config.AuthToken != "" {
				j, _ := json.Marshal(map[string]interface{}{
					"type":  "auth",
					"token": config.AuthToken,
				})
				d.SendText(string(j))
			}
		},
		OnMessageFunc: func(d *webrtc.DataChannel, msg webrtc.DataChannelMessage) {
			var auth struct {
				Type   string `json:"type"`
				Result bool   `json:"result"`
			}
			_ = json.Unmarshal(msg.Data, &auth)
			if auth.Type == "authResult" {
				authorized = auth.Result
				wg.Done()
			}
		},
	}}

	rtcConn.Start(dataChannels)

	log.Println("connectiong...")
	wg.Wait()
	log.Println("connected!", authorized)
	if authorized {
		go func() {
			fs.WalkDir(client, "/", func(path string, d fs.DirEntry, err error) error {
				log.Println(path)
				return nil
			})
			rtcConn.Close()
		}()
	} else {
		rtcConn.Close()
	}
	return rtcConn.Wait(ctx)
}

func Pairing(ctx context.Context, config *Config) error {
	pin, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return err
	}
	ctx, done := context.WithTimeout(ctx, time.Duration(config.PairingTimeoutSec)*time.Second)
	defer done()

	pinstr := fmt.Sprintf("%06d", pin)
	log.Println("PIN: ", pinstr)

	rtcConn, err := NewRTCConn(config.SignalingUrl, config.PairingRoomIdPrefix+pinstr, config.SignalingKey)
	if err != nil {
		return err
	}
	defer rtcConn.Close()
	if rtcConn.ayameConn.AuthResult.IsExistClient {
		return errors.New("room already used")
	}

	dataChannels := []DataChannelHandler{&DataChannelCallback{
		Name: "secretExchange",
		OnOpenFunc: func(dc *webrtc.DataChannel) {
			j, _ := json.Marshal(map[string]interface{}{
				"type":         "hello",
				"roomId":       config.RoomIdPrefix + config.RoomName,
				"signalingKey": config.SignalingKey,
				"token":        config.AuthToken,
				"userAgent":    "rtcfs",
				"version":      1,
			})
			dc.SendText(string(j))
		},
		OnMessageFunc: func(d *webrtc.DataChannel, msg webrtc.DataChannelMessage) {
			d.OnMessage(func(msg webrtc.DataChannelMessage) {
				// TODO: Save credentials
				log.Println(string(msg.Data))
				rtcConn.Close()
			})
		},
	}}

	rtcConn.Start(dataChannels)
	return rtcConn.Wait(ctx)
}

func main() {
	confPath := flag.String("conf", "config.toml", "conf path")
	roomName := flag.String("room", "", "Ayame room name")
	authToken := flag.String("token", "", "auth token")
	localPath := flag.String("path", ".", "local path to share")
	flag.Parse()

	config := loadConfig(*confPath)
	if *localPath != "" {
		config.LocalPath = *localPath
	}
	if *roomName != "" {
		config.RoomName = *roomName
	}
	if *authToken != "" {
		config.AuthToken = *authToken
	}

	if config.ThumbnailCacheDir != "" {
		DefaultThumbnailer.Register(NewImageThumbnailer(config.ThumbnailCacheDir))
		if config.FFmpegPath != "" {
			DefaultThumbnailer.Register(NewVideoThumbnailer(config.ThumbnailCacheDir, config.FFmpegPath))
		}
	}

	switch flag.Arg(0) {
	case "pairing":
		err := Pairing(context.Background(), config)
		if err != nil {
			log.Println(err)
		}
	case "traverse-test":
		err := TraverseForTest(context.Background(), config)
		if err != nil {
			log.Println(err)
		}
	case "publish", "":
		for {
			err := PublishFiles(context.Background(), config)
			if err != nil {
				log.Println("ERROR:", err)
			}
			time.Sleep(5 * time.Second)
		}
	default:
		fmt.Println("Unknown sub command: ", flag.Arg(0))
		flag.Usage()
	}
}
