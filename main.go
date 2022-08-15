package main

import (
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

func PublishFiles(config *Config) error {
	rtcConn, err := NewRTCConn(config.SignalingUrl, config.RoomIdPrefix+config.RoomName+".1", config.SignalingKey)
	if err != nil {
		return err
	}
	defer func() {
		if err := rtcConn.Close(); err != nil {
			log.Printf("cannot close peerConnection: %v\n", err)
		}
	}()

	var fileHander = NewFileHandler(os.DirFS(config.LocalPath))
	initFileHandler := func(d *webrtc.DataChannel, handler *FileHandler) {
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
	return rtcConn.Wait()
}

func Pairing(config *Config) error {
	pin, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return err
	}
	pinstr := fmt.Sprintf("%06d", pin)

	log.Println("PIN: ", pinstr)
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
	// TODO timeout

	rtcConn.Start()
	return rtcConn.Wait()
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

	if flag.Arg(0) == "pairing" {
		err := Pairing(config)
		if err != nil {
			log.Println(err)
		}
		return
	}

	if config.ThumbnailCacheDir != "" {
		DefaultThumbnailer.Register(NewImageThumbnailer(config.ThumbnailCacheDir))
		if config.FFmpegPath != "" {
			DefaultThumbnailer.Register(NewVideoThumbnailer(config.ThumbnailCacheDir, config.FFmpegPath))
		}
	}

	for {
		err := PublishFiles(config)
		if err != nil {
			log.Println(err)
		}
		time.Sleep(5 * time.Second)
	}
}
