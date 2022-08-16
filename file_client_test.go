package main

import (
	"context"
	"io/fs"
	"os"
	"testing"
)

func newFakeClient() *FSClient {
	ctx := context.Background()
	// Connect server and client directly
	var client *FSClient
	server := NewFSServer(os.DirFS("testdata"), 1)
	client = NewFSClient(func(req *FileOperationRequest) error {
		return server.HandleMessage(ctx, req.ToBytes(), true, func(res *FileOperationResult) error {
			return client.HandleMessage(res.ToBytes(), res.IsJSON())
		})
	})
	return client
}

func TestFSClient_Dir(t *testing.T) {
	client := newFakeClient()
	defer client.Abort()

	files, err := client.ReadDir("/")
	if err != nil || files == nil {
		t.Fatal("ReadDir() error: ", err)
	}

	f, err := client.Open("/")
	if err != nil || f == nil {
		t.Fatal("Open() dir error: ", err)
	}
	stat, err := f.Stat()
	if err != nil || stat == nil {
		t.Fatal("file Stat() error: ", err)
	}
	f.Close()

	t.Log("Name(): ", stat.Name())
	t.Log("Size(): ", stat.Size())
	t.Log("IsDir(): ", stat.IsDir())
	t.Log("ModTime(): ", stat.ModTime())
	t.Log("Mode(): ", stat.Mode())
	t.Log("Sys(): ", stat.Sys())

	if stat.IsDir() != true {
		t.Fatal("IsDir() should be true ")
	}
}

func TestFSClient_File(t *testing.T) {
	client := newFakeClient()
	defer client.Abort()

	fname := "/test.png"

	f, err := client.Open(fname)
	if err != nil || f == nil {
		t.Fatal("Open() file error: ", err)
	}
	stat, err := f.Stat()
	f.Close()

	t.Log("Name(): ", stat.Name())
	t.Log("Size(): ", stat.Size())
	t.Log("IsDir(): ", stat.IsDir())
	t.Log("ModTime(): ", stat.ModTime())
	t.Log("Mode(): ", stat.Mode())
	t.Log("Sys(): ", stat.Sys())

	if stat.IsDir() != false {
		t.Fatal("IsDir() should be false ")
	}

	data, err := fs.ReadFile(client, fname)
	if err != nil {
		t.Error("ReadFile() error: ", err)
	}
	if len(data) != int(stat.Size()) {
		t.Error("ReadFile() size error: ", len(data), stat.Size())
	}
}
