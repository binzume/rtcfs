package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"

	"github.com/BurntSushi/toml"
	"github.com/binzume/fsmount"
	"github.com/binzume/webrtcfs/rtcfs"
)

type Config struct {
	SignalingUrl string
	SignalingKey string
	RoomIdPrefix string

	RoomName  string
	AuthToken string
}

func DefaultConfig() *Config {
	var config Config
	config.SignalingUrl = "wss://ayame-labo.shiguredo.app/signaling"
	config.SignalingKey = "VV69g7Ngx-vNwNknLhxJPHs9FpRWWNWeUzJ9FUyylkD_yc_F"
	config.RoomIdPrefix = "binzume@rdp-room-"
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

func main() {
	confPath := flag.String("conf", "config.toml", "conf path")
	roomName := flag.String("room", "", "Ayame room name")
	authToken := flag.String("token", "", "auth token")
	flag.Parse()

	config := loadConfig(*confPath)
	if *roomName != "" {
		config.RoomName = *roomName
	}
	if *authToken != "" {
		config.AuthToken = *authToken
	}

	mountpoint := "X:"
	if flag.Arg(0) != "" {
		mountpoint = flag.Arg(0)
	}

	options := &rtcfs.ConnectOptions{
		SignalingURL: config.SignalingUrl,
		SignalingKey: config.SignalingKey,
		RoomID:       config.RoomIdPrefix + config.RoomName + ".1",
		AuthToken:    config.AuthToken,
	}

	rtcConn, client, err := rtcfs.GetClinet(context.Background(), options, &rtcfs.ClientOptions{MaxRedirect: 3})
	if err != nil {
		log.Fatal(err)
	}
	defer rtcConn.Close()

	m, _ := fsmount.MountFS(mountpoint, client, nil)
	defer m.Close()

	select {}
}
