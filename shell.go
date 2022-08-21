package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/pion/webrtc/v3"
)

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

func shellListFiles(ctx context.Context, fsys fs.FS, cwd, arg string) error {
	printInfo := func(d fs.DirEntry, path string) {
		finfo, _ := d.Info()
		ent := NewFileEntry(finfo, true)
		fmt.Println(ent.Mode(), "\t", ent.Size(), "\t", ent.Type, "\t", path)
	}
	fpath := path.Join(cwd, arg)
	if strings.HasSuffix(fpath, "/**") {
		return fs.WalkDir(fsys, strings.TrimSuffix(fpath, "/**"), func(path string, d fs.DirEntry, err error) error {
			printInfo(d, path)
			return err
		})
	} else if fsys, ok := fsys.(OpenDirFS); ok {
		dir, err := fsys.OpenDir(fpath)
		if err != nil {
			return err
		}
		for {
			files, err := dir.ReadDir(200)
			for _, f := range files {
				printInfo(f, f.Name())
			}
			if err != nil {
				if err == io.EOF {
					err = nil
				}
				return err
			}
		}
	} else if fsys, ok := fsys.(fs.ReadDirFS); ok {
		files, err := fsys.ReadDir(fpath)
		for _, f := range files {
			printInfo(f, f.Name())
		}
		return err
	} else {
		return errors.New("not implemented")
	}
}

func shellCat(ctx context.Context, fsys fs.FS, cwd, arg string) error {
	fpath := path.Join(cwd, arg)
	stat, err := fs.Stat(fsys, fpath)
	if err != nil {
		return err
	}
	log.Println("Pull: ", fpath, " (", stat.Size(), "B)")
	r, err := fsys.Open(fpath)
	if err != nil {
		return err
	}
	defer r.Close()
	_, err = io.Copy(os.Stdout, r)
	return err
}

func shellPullFile(ctx context.Context, fsys fs.FS, cwd, arg string) error {
	fpath := path.Join(cwd, arg)
	stat, err := fs.Stat(fsys, fpath)
	if err != nil {
		return err
	}
	log.Println("Pull: ", fpath, " (", stat.Size(), "B)")
	r, err := fsys.Open(fpath)
	if err != nil {
		return err
	}
	defer r.Close()
	w, err := os.Create(filepath.Base(fpath))
	if err != nil {
		return err
	}
	defer w.Close()
	_, err = io.Copy(w, r)
	return err
}

func shellPushFile(ctx context.Context, fsys *FSClient, cwd, arg string) error {
	r, err := os.Open(arg)
	if err != nil {
		return err
	}
	defer r.Close()
	stat, err := r.Stat()
	if err != nil {
		return err
	}
	log.Println("Push: ", arg, " (", stat.Size(), "B)")

	fpath := path.Join(cwd, path.Base(arg))
	w, err := fsys.Create(fpath)
	if err != nil {
		return err
	}
	defer w.Close()
	_, err = io.Copy(w, r)
	return err
}

func shellExecCmd(ctx context.Context, client *FSClient, cwd, cmd, arg string) error {
	switch cmd {
	case "":
		return nil
	case "pwd":
		fmt.Println(cwd)
		return nil
	case "ls":
		return shellListFiles(ctx, client, cwd, arg)
	case "pull":
		return shellPullFile(ctx, client, cwd, arg)
	case "cat":
		return shellCat(ctx, client, cwd, arg)
	case "push":
		return shellPushFile(ctx, client, cwd, arg)
	case "rm":
		return client.Remove(path.Join(cwd, arg))
	case "?", "help":
		fmt.Println("Commands: exit, pwd, cd PATH, ls PATH, pull FILE, push FILE, cat FILE, rm FILE")
		return nil
	default:
		return errors.New("No such command: " + cmd)
	}
}

func ShellExec(ctx context.Context, config *Config, cmd, arg string) error {
	rtcConn, client, err := getClinet(ctx, config)
	if err != nil {
		return err
	}
	defer rtcConn.Close()
	return shellExecCmd(ctx, client, "/", cmd, arg)
}

func StartShell(ctx context.Context, config *Config) error {
	rtcConn, client, err := getClinet(ctx, config)
	if err != nil {
		return err
	}
	defer rtcConn.Close()

	cwd := "/"
	shellExecCmd(ctx, client, cwd, "help", "")
	s := bufio.NewScanner(os.Stdin)
	for s.Scan() {
		cmd := strings.SplitN(s.Text(), " ", 2)
		arg := ""
		if len(cmd) > 1 {
			arg = cmd[1]
		}
		if cmd[0] == "exit" {
			return nil
		} else if cmd[0] == "cd" {
			cwd = path.Join(cwd, arg)
		} else {
			err := shellExecCmd(ctx, client, cwd, cmd[0], arg)
			if err != nil {
				fmt.Println("ERROR: ", err)
			}
		}
		select {
		case <-ctx.Done():
			return nil
		default:
		}
	}
	return nil
}
