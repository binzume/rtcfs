package rtcfs

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"io/fs"
	"log"
	"time"

	"github.com/binzume/webrtcfs/socfs"
	"github.com/pion/webrtc/v3"
)

func randomStr(l int) string {
	const letters = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	b := make([]byte, l)
	r := make([]byte, l)
	if _, err := rand.Read(r); err != nil {
		panic(err)
	}
	for i := range b {
		b[i] = letters[r[i]%byte(len(letters))]
	}
	return string(b)
}

func StartRedirector(ctx context.Context, options *ConnectOptions, redirect func(roomId string)) error {
	for {
		rtcConn, err := NewRTCConn(options.SignalingURL, options.DefaultRoomID(), options.SignalingKey)
		if err != nil {
			return err
		}

		dataChannels := []DataChannelHandler{&DataChannelCallback{
			Name: "controlEvent",
			OnOpenFunc: func(d *webrtc.DataChannel) {
				roomID := options.RoomID + "." + randomStr(10)
				go redirect(roomID)
				j, _ := json.Marshal(map[string]interface{}{
					"type":   "redirect",
					"roomId": roomID,
				})
				d.SendText(string(j))
				go func() {
					time.Sleep(time.Second)
					rtcConn.Close()
				}()
			},
		}}

		rtcConn.Start(dataChannels)
		rtcConn.Wait(ctx)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func Publish(ctx context.Context, options *ConnectOptions, fsys fs.FS) error {
	return PublishRoomID(ctx, options, options.DefaultRoomID(), fsys)
}

func PublishRoomID(ctx context.Context, options *ConnectOptions, roomID string, fsys fs.FS) error {
	password := options.Password
	authorized := password == ""

	rtcConn, err := NewRTCConn(options.SignalingURL, roomID, options.SignalingKey)
	if err != nil {
		return err
	}
	defer func() {
		if err := rtcConn.Close(); err != nil {
			log.Printf("cannot close peerConnection: %v\n", err)
		}
	}()

	fileHander := socfs.NewFSServer(fsys, 8)

	dataChannels := []DataChannelHandler{&DataChannelCallback{
		Name: "fileServer",
		OnMessageFunc: func(d *webrtc.DataChannel, msg webrtc.DataChannelMessage) {
			if !authorized {
				fileHander.ErrorReply(ctx, msg.Data, msg.IsString, func(res *socfs.FileOperationResult) error {
					if res.IsJSON() {
						return d.SendText(string(res.ToBytes()))
					} else {
						return d.Send(res.ToBytes())
					}
				}, "auth error")
				return
			}
			fileHander.HandleMessage(ctx, msg.Data, msg.IsString, func(res *socfs.FileOperationResult) error {
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
				Type       string `json:"type"`
				Token      string `json:"token"` // TODO: Remove this
				Fingeprint string `json:"fingerprint"`
				Hmac       []byte `json:"hmac"`
			}
			_ = json.Unmarshal(msg.Data, &auth)
			if auth.Type == "auth" {
				if len(auth.Hmac) > 0 {
					if !rtcConn.ValidateRemoteFingerprint(auth.Fingeprint) {
						// Broken client or MITM
						log.Println("fingerprint error: ", auth.Fingeprint)
					} else {
						h := hmac.New(sha256.New, []byte(options.Password))
						h.Write([]byte(auth.Fingeprint))
						authorized = authorized || bytes.Compare(h.Sum(nil), auth.Hmac) == 0
					}
				} else {
					authorized = authorized || auth.Token == password
				}
				log.Println("auth result:", authorized, string(msg.Data))
				j, _ := json.Marshal(map[string]interface{}{
					"type":     "authResult",
					"result":   authorized,
					"services": map[string]interface{}{"file": fileHander.FSCaps()},
				})
				d.SendText(string(j))
			}
		},
	}}

	rtcConn.Start(dataChannels)
	return rtcConn.Wait(ctx)
}
