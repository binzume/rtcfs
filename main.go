package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/binzume/cfs/zipfs"
	"github.com/binzume/webrtcfs/rtcfs"
	"github.com/binzume/webrtcfs/socfs"
)

type Config struct {
	SignalingUrl        string
	SignalingKey        string
	RoomIdPrefix        string
	PairingRoomIdPrefix string
	PairingTimeoutSec   int

	Name      string
	Password  string
	LocalPath string

	Writable bool
	Unzip    bool

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
	config.FFmpegPath = os.Getenv("FFMPEG_PATH")
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

func publishFiles(ctx context.Context, config *Config, options *rtcfs.ConnectOptions) error {
	if config.ThumbnailCacheDir != "" {
		socfs.DefaultThumbnailer.Register(socfs.NewImageThumbnailer(config.ThumbnailCacheDir))
		if config.FFmpegPath != "" {
			socfs.DefaultThumbnailer.Register(socfs.NewVideoThumbnailer(config.ThumbnailCacheDir, config.FFmpegPath))
		}
	}
	var fsys fs.FS = socfs.NewWritableDirFS(config.LocalPath)
	if config.Unzip {
		fsys = zipfs.NewAutoUnzipFS(fsys)
		socfs.ContentTypes[".zip"] = "application/zip;x-traversable"
	}

	wfsys := socfs.WrapFS(fsys)
	if !config.Writable {
		wfsys.ReadOnly()
	}
	log.Println("connecting... ", options.RoomID)
	return rtcfs.StartRedirector(ctx, options, func(roomID string) {
		// TODO: connect timeout
		log.Println("temporary room:", roomID)
		rtcfs.PublishRoomID(ctx, options, roomID, wfsys)
	})
}

func main() {
	confPath := flag.String("conf", "config.toml", "conf path")
	name := flag.String("room", "", "Room name")
	displayName := flag.String("name", "rtcfs", "Display name(pairing)")
	password := flag.String("passwd", "", "Connect password")
	signalingUrl := flag.String("signalingUrl", "", "Ayame signaling url")
	signalingKey := flag.String("signalingKey", "", "Ayame signaling key")
	writable := flag.Bool("writable", false, "writable fs")
	unzip := flag.Bool("unzip", false, "Allow ReadDir() for .zip file(experiment)")
	flag.Parse()

	config := loadConfig(*confPath)
	if *name != "" {
		config.Name = *name
	}
	if *password != "" {
		config.Password = *password
	}
	if *signalingUrl != "" {
		config.SignalingUrl = *signalingUrl
	}
	if *signalingKey != "" {
		config.SignalingKey = *signalingKey
	}
	if *writable {
		config.Writable = *writable
	}
	if *unzip {
		config.Unzip = *unzip
	}

	options := &rtcfs.ConnectOptions{
		SignalingURL: config.SignalingUrl,
		SignalingKey: config.SignalingKey,
		RoomID:       config.RoomIdPrefix + config.Name,
		Password:     config.Password,
	}

	switch flag.Arg(0) {
	case "pairing":
		log.Println("Pairing... room:", options.RoomID)
		err := rtcfs.Pairing(context.Background(), &rtcfs.PairingOptions{
			ConnectOptions:      *options,
			PairingRoomIDPrefix: config.PairingRoomIdPrefix,
			Timeout:             time.Duration(config.PairingTimeoutSec) * time.Second,
			DisplayName:         *displayName,
		})
		if err != nil {
			log.Println(err)
		}
	case "shell":
		err := rtcfs.StartShell(context.Background(), options)
		if err != nil {
			log.Println(err)
		}
	case "pull", "push", "ls", "cat", "rm", "mkdir":
		err := rtcfs.ShellExec(context.Background(), options, flag.Arg(0), flag.Arg(1))
		if err != nil {
			log.Println(err)
		}
	case "publish":
		if flag.Arg(1) != "" {
			config.LocalPath = flag.Arg(1)
		}
		for {
			err := publishFiles(context.Background(), config, options)
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
