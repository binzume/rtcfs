package rtcfs

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/big"
	"time"

	"github.com/pion/webrtc/v3"
)

type PairingOptions struct {
	ConnectOptions
	PairingRoomIDPrefix string
	Timeout             time.Duration
	DisplayName         string
}

func Pairing(ctx context.Context, options *PairingOptions) error {
	pin, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return err
	}
	ctx, done := context.WithTimeout(ctx, options.Timeout)
	defer done()

	pinstr := fmt.Sprintf("%06d", pin)
	log.Println("PIN: ", pinstr)

	rtcConn, err := NewRTCConn(options.SignalingURL, options.PairingRoomIDPrefix+pinstr, options.SignalingKey)
	if err != nil {
		return err
	}
	defer rtcConn.Close()
	if rtcConn.IsExistRoom() {
		return errors.New("room already used")
	}

	dataChannels := []DataChannelHandler{&DataChannelCallback{
		Name: "secretExchange",
		OnOpenFunc: func(dc *webrtc.DataChannel) {
			j, _ := json.Marshal(map[string]interface{}{
				"type":         "hello",
				"roomId":       options.RoomID,
				"signalingKey": options.SignalingKey,
				"token":        options.Password,
				"name":         options.DisplayName,
				"userAgent":    "rtcfs",
				"services":     []string{"file", "no-client"},
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
