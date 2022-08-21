package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"math/big"
	"os"
	"path/filepath"
	"strings"
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

func getClinet(ctx context.Context, config *Config) (*RTCConn, *FSClient, error) {
	roomID := config.RoomIdPrefix + config.RoomName + ".1"
	return getClinetInternal(ctx, config, roomID, 0)
}

func getClinetInternal(ctx context.Context, config *Config, roomID string, redirectCount int) (*RTCConn, *FSClient, error) {
	log.Println("waiting for connect: ", roomID)
	var client *FSClient
	authorized := config.AuthToken == ""

	rtcConn, err := NewRTCConn(config.SignalingUrl, roomID, config.SignalingKey)
	if err != nil {
		return nil, nil, err
	}

	var wg sync.WaitGroup
	wg.Add(1)
	if config.AuthToken != "" {
		wg.Add(1)
	}

	var redirect string

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
		OnCloseFunc: func(d *webrtc.DataChannel) {
			client.Abort()
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
			var event struct {
				Type   string `json:"type"`
				Result bool   `json:"result"`
				RoomID string `json:"roomId"`
			}
			_ = json.Unmarshal(msg.Data, &event)
			if event.Type == "authResult" {
				authorized = event.Result
				wg.Done()
			} else if event.Type == "redirect" {
				redirect = event.RoomID
				wg.Done()
			}
		},
	}}

	rtcConn.Start(dataChannels)

	log.Println("connectiong...")
	wg.Wait()

	if redirect != "" {
		rtcConn.Close()
		log.Println("redirect to roomId:", redirect)
		if redirectCount > 3 {
			return nil, nil, errors.New("too may redirect")
		}
		return getClinetInternal(ctx, config, redirect, redirectCount+1)
	}

	log.Println("connected! ", authorized)
	if !authorized {
		rtcConn.Close()
		return nil, nil, errors.New("auth error")
	}
	return rtcConn, client, nil
}

func ListFiles(ctx context.Context, config *Config, path string) error {
	rtcConn, client, err := getClinet(ctx, config)
	if err != nil {
		return err
	}
	defer rtcConn.Close()
	if strings.HasSuffix(path, "/**") {
		return fs.WalkDir(client, strings.TrimSuffix(path, "/**"), func(path string, d fs.DirEntry, err error) error {
			finfo, _ := d.Info()
			ent := NewFileEntry(finfo, true)
			fmt.Println(ent.Mode(), "\t", ent.Size(), "\t", ent.Type, "\t", path)
			return err
		})
	} else {
		dir, err := client.OpenDir(path)
		if err != nil {
			return err
		}
		for {
			files, err := dir.ReadDir(200)
			for _, file := range files {
				finfo, _ := file.Info()
				ent := NewFileEntry(finfo, true)
				fmt.Println(ent.Mode(), "\t", ent.Size(), "\t", ent.Type, "\t", ent.Name())
			}
			if err != nil {
				break
			}
		}
	}

	return err
}

func pullFile(ctx context.Context, config *Config, path string) error {
	rtcConn, client, err := getClinet(ctx, config)
	if err != nil {
		return err
	}
	defer rtcConn.Close()
	stat, err := client.Stat(path)
	if err != nil {
		return err
	}
	log.Println("Size: ", stat.Size())
	r, err := client.Open(path)
	if err != nil {
		return err
	}
	defer r.Close()
	w, err := os.Create(filepath.Base(path))
	if err != nil {
		return err
	}
	defer w.Close()
	_, err = io.Copy(w, r)
	return err
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
	case "ls":
		err := ListFiles(context.Background(), config, flag.Arg(1))
		if err != nil {
			log.Println(err)
		}
	case "pull":
		err := pullFile(context.Background(), config, flag.Arg(1))
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
