package rtcfs

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"log"
	"sync"

	"github.com/binzume/webrtcfs/socfs"
	"github.com/pion/webrtc/v3"
)

type ClientOptions struct {
	MaxRedirect int
}

func GetClinet(ctx context.Context, options *ConnectOptions, clientOpt *ClientOptions) (*RTCConn, *socfs.FSClient, error) {
	return getClinetInternal(ctx, options, options.DefaultRoomID(), clientOpt.MaxRedirect)
}

func getClinetInternal(ctx context.Context, options *ConnectOptions, roomID string, redirectCount int) (*RTCConn, *socfs.FSClient, error) {
	log.Println("waiting for connect: ", roomID)
	var client *socfs.FSClient
	authorized := options.Password == ""

	rtcConn, err := NewRTCConn(options.SignalingURL, roomID, options.SignalingKey)
	if err != nil {
		return nil, nil, err
	}

	var wg sync.WaitGroup
	wg.Add(2)

	var redirect string
	var services map[string]interface{}

	dataChannels := []DataChannelHandler{&DataChannelCallback{
		Name: "fileServer",
		OnOpenFunc: func(dc *webrtc.DataChannel) {
			client = socfs.NewFSClient(func(req *socfs.FileOperationRequest) error {
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
			fingerprint, _ := rtcConn.LocalCertificateFingerprint()
			h := hmac.New(sha256.New, []byte(options.Password))
			h.Write([]byte(fingerprint))
			j, _ := json.Marshal(map[string]interface{}{
				"type": "auth",
				// "token":       options.AuthToken, // TODO: Remove this
				"fingerprint": fingerprint,
				"hmac":        h.Sum(nil), // base64 string in json
			})
			d.SendText(string(j))
		},
		OnMessageFunc: func(d *webrtc.DataChannel, msg webrtc.DataChannelMessage) {
			var event struct {
				Type     string                 `json:"type"`
				Result   bool                   `json:"result"`
				RoomID   string                 `json:"roomId"`
				Services map[string]interface{} `json:"services"`
			}
			_ = json.Unmarshal(msg.Data, &event)
			if event.Type == "authResult" {
				authorized = event.Result
				services = event.Services
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
		if redirectCount <= 0 {
			return nil, nil, errors.New("too may redirect")
		}
		return getClinetInternal(ctx, options, redirect, redirectCount-1)
	}

	log.Println("connected! ", authorized)

	if services != nil && services["file"] == nil {
		rtcConn.Close()
		return nil, nil, errors.New("no file service")
	}

	if !authorized {
		rtcConn.Close()
		return nil, nil, errors.New("auth error")
	}
	return rtcConn, client, nil
}
