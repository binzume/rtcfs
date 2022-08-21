package main

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"testing"
)

func newFakeClient(fsys fs.FS) *FSClient {
	ctx := context.Background()
	// Connect server and client directly
	var client *FSClient
	server := NewFSServer(fsys, 1)
	client = NewFSClient(func(req *FileOperationRequest) error {
		return server.HandleMessage(ctx, req.ToBytes(), true, func(res *FileOperationResult) error {
			return client.HandleMessage(res.ToBytes(), res.IsJSON())
		})
	})
	return client
}

func TestFSClient_Dir(t *testing.T) {
	client := newFakeClient(os.DirFS("testdata"))
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
	client := newFakeClient(os.DirFS("testdata"))
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
	client := newFakeClient(os.DirFS("testdata"))
	defer client.Abort()

	fname := "/test.png"

	f, err := client.Open(fname)
	if err != nil || f == nil {
		t.Fatal("Open() file error: ", err)
	}
	f.Close()

	stat, err := f.Stat()
	if err != nil {
		t.Fatal("Stat() file error: ", err)
	}

	data, err := fs.ReadFile(client, fname)
	if err != nil {
		t.Error("ReadFile() error: ", err)
	}
	if len(data) != int(stat.Size()) {
		t.Error("ReadFile() size error: ", len(data), stat.Size())
	}
}

func TestFSClient_Write(t *testing.T) {
	fsys := NewWritableDirFS("testdata")
	client := newFakeClient(fsys)
	defer client.Abort()

	fname := "test.txt"

	// Create and Write
	w, err := client.Create(fname)
	if err != nil {
		t.Fatal("Failed to truncate", err)
	}
	w.Write([]byte("Hello!"))
	w.Close()

	stat, err := client.Stat(fname)
	if err != nil {
		t.Fatal("Stat() file error: ", err)
	}
	if stat.Size() != int64(len("Hello!")) {
		t.Fatal("Size error ", stat.Size())
	}

	// Truncate
	err = client.Truncate(fname, 0)
	if err != nil {
		t.Fatal("Failed to truncate", err)
	}

	stat, err = client.Stat(fname)
	if err != nil {
		t.Fatal("Stat() file error: ", err)
	}
	if stat.Size() != 0 {
		t.Fatal("Size error ", stat.Size())
	}

	// Remove
	err = client.Remove(fname)
	if err != nil {
		t.Fatal("Failed to remove", err)
	}

	stat, err = client.Stat(fname)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("Stat() should be ErrNotExist: ", err)
	}

	// Readonly
	fsys.Capability().Create = false
	fsys.Capability().Remove = false
	fsys.Capability().Write = false

	err = client.Truncate(fname, 0)
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatal("truncate should be failed with permission error: ", err)
	}
}
