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
	rtcConn, err := NewRTCConn(config.SignalingUrl, roomID, config.SignalingKey)
	if err != nil {
		return err
	}
	defer func() {
		if err := rtcConn.Close(); err != nil {
			log.Printf("cannot close peerConnection: %v\n", err)
		}
	}()
	ctx, done := context.WithCancel(ctx)
	defer done()

	fileHander := NewFileHandler(os.DirFS(config.LocalPath), 8)
	initFileHandler := func(d *webrtc.DataChannel, handler *FileHandler) {
		d.OnMessage(func(msg webrtc.DataChannelMessage) {
			handler.HandleMessage(ctx, msg.Data, msg.IsString, func(res *FileOperationResult) {
				if res.IsBinary() {
					d.Send(res.ToBytes())
				} else {
					d.SendText(string(res.ToBytes()))
				}
			})
		})
	}

	rtcConn.PC.OnDataChannel(func(d *webrtc.DataChannel) {
		log.Printf("New DataChannel %s %d\n", d.Label(), d.ID())
		if d.Label() == "fileServer" {
			log.Printf("Start file server!")
			initFileHandler(d, fileHander)
		}
	})

	// TODO
	if rtcConn.ayameConn.AuthResult.IsExistClient {
		d, _ := rtcConn.PC.CreateDataChannel("fileServer", nil)
		initFileHandler(d, fileHander)
	}

	rtcConn.Start()
	return rtcConn.Wait(ctx)
}

func Pairing(ctx context.Context, config *Config) error {
	pin, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return err
	}
	pinstr := fmt.Sprintf("%06d", pin)
	log.Println("PIN: ", pinstr)

	ctx, done := context.WithTimeout(ctx, time.Duration(config.PairingTimeoutSec)*time.Second)
	defer done()

	rtcConn, err := NewRTCConn(config.SignalingUrl, config.PairingRoomIdPrefix+pinstr, config.SignalingKey)
	if err != nil {
		return err
	}
	defer func() {
		if err := rtcConn.Close(); err != nil {
			log.Printf("cannot close peerConnection: %v\n", err)
		}
	}()

	rtcConn.PC.OnDataChannel(func(d *webrtc.DataChannel) {
		log.Printf("New DataChannel %s %d\n", d.Label(), d.ID())
		if d.Label() == "secretExchange" {
			d.OnOpen(func() {
				j, _ := json.Marshal(map[string]interface{}{
					"type":         "hello",
					"roomId":       config.RoomIdPrefix + config.RoomName,
					"signalingKey": config.SignalingKey,
					"userAgent":    "rtcfs",
					"version":      1,
				})
				d.SendText(string(j))
			})
			d.OnMessage(func(msg webrtc.DataChannelMessage) {
				// TODO: Save credentials
				log.Println(string(msg.Data))
				rtcConn.Close()
			})
		}
	})

	rtcConn.Start()
	return rtcConn.Wait(ctx)
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

	switch flag.Arg(0) {
	case "pairing":
		err := Pairing(context.Background(), config)
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
