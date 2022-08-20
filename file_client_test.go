package main

import (
	"context"
	"errors"
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
	f.Close()

}

func TestFSClient_Stat(t *testing.T) {
	client := newFakeClient()
	defer client.Abort()

	// Dir
	stat, err := client.Stat("/")
	if err != nil || stat == nil {
		t.Fatal("Stat() file error: ", err)
	}
	if stat.IsDir() != true {
		t.Fatal("IsDir() should be true ")
	}
	t.Log("Name(): ", stat.Name())
	t.Log("Size(): ", stat.Size())
	t.Log("IsDir(): ", stat.IsDir())
	t.Log("ModTime(): ", stat.ModTime())
	t.Log("Mode(): ", stat.Mode())
	t.Log("Sys(): ", stat.Sys())

	// Normal file
	stat, err = client.Stat("/test.png")
	if err != nil || stat == nil {
		t.Fatal("Stat() file error: ", err)
	}
	if stat.IsDir() != false {
		t.Fatal("IsDir() should be false ")
	}
	t.Log("Name(): ", stat.Name())
	t.Log("Size(): ", stat.Size())
	t.Log("IsDir(): ", stat.IsDir())
	t.Log("ModTime(): ", stat.ModTime())
	t.Log("Mode(): ", stat.Mode())
	t.Log("Sys(): ", stat.Sys())

	// Not found
	stat, err = client.Stat("/not_exist_file")
	if err == nil || !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("Stat() should be ErrNotExist: ", err)
	}
	if stat != nil {
		t.Fatal("stat shoudl be nil ", stat)
	}
}

func TestFSClient_File(t *testing.T) {
	client := newFakeClient()
	defer client.Abort()

	fname := "/test.png"

	stat, err := client.Stat(fname)
	if err != nil {
		t.Fatal("Stat() file error: ", err)
	}

	f, err := client.Open(fname)
	if err != nil || f == nil {
		t.Fatal("Open() file error: ", err)
	}
	f.Close()

	data, err := fs.ReadFile(client, fname)
	if err != nil {
		t.Error("ReadFile() error: ", err)
	}
	if len(data) != int(stat.Size()) {
		t.Error("ReadFile() size error: ", len(data), stat.Size())
	}
}
