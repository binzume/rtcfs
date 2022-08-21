package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/big"
	"os"
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

	Writable bool

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

	fsys := NewWritableDirFS(config.LocalPath)
	if !config.Writable {
		fsys.Capability().Create = false
		fsys.Capability().Remove = false
		fsys.Capability().Write = false
	}
	fileHander := NewFSServer(fsys, 8)

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
				authorized = authorized || auth.Token == config.AuthToken
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
	writable := flag.Bool("writable", false, "writable fs")
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
	if *writable {
		config.Writable = *writable
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
	case "shell":
		err := StartShell(context.Background(), config)
		if err != nil {
			log.Println(err)
		}
	case "pull", "push", "ls", "cat", "rm":
		err := ShellExec(context.Background(), config, flag.Arg(0), flag.Arg(1))
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
