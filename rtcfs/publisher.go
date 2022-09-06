package rtcfs

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"io/fs"
	"log"

	"github.com/binzume/webrtcfs/socfs"
	"github.com/pion/webrtc/v3"
)

func Publish(ctx context.Context, options *ConnectOptions, fsys fs.FS) error {
	authToken := options.AuthToken
	authorized := authToken == ""

	rtcConn, err := NewRTCConn(options.SignalingURL, options.RoomID, options.SignalingKey)
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
						h := hmac.New(sha256.New, []byte(options.AuthToken))
						h.Write([]byte(auth.Fingeprint))
						authorized = authorized || bytes.Compare(h.Sum(nil), auth.Hmac) == 0
					}
				} else {
					authorized = authorized || auth.Token == authToken
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
