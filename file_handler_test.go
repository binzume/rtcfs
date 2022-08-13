package main

import (
	"io/fs"
	"os"
	"testing"
)

func TestFileHandler_files(t *testing.T) {
	fileHander := NewFileHandler(os.DirFS("."))
	ret, err := fileHander.HanldeFileOp(&FileOperation{Op: "files", Path: "/"})
	if err != nil {
		t.Fatal(err)
	}
	if files, ok := ret.([]*FileEntry); ok {
		t.Log(files)
	} else {
		t.Error("type error", ret)
	}
}

func TestFileHandler_stat(t *testing.T) {
	fileHander := NewFileHandler(os.DirFS("."))
	ret, err := fileHander.HanldeFileOp(&FileOperation{Op: "stat", Path: "/LICENSE"})
	if err != nil {
		t.Fatal(err)
	}
	if ent, ok := ret.(*FileEntry); ok {
		t.Log(ent)
	} else {
		t.Error("type error", ret)
	}
}

func TestFileHandler_read(t *testing.T) {
	fileHander := NewFileHandler(os.DirFS("."))
	ret, err := fileHander.HanldeFileOp(&FileOperation{Op: "read", Path: "/LICENSE", Pos: 10, Len: 10})
	if err != nil {
		t.Fatal(err)
	}
	if data, ok := ret.([]byte); ok {
		t.Log(data)
	} else {
		t.Error("type error", ret)
	}
}

type fakeWritableFs struct {
	fs.FS
}

func (f fakeWritableFs) Remove(path string) error {
	_, err := fs.Stat(f.FS, path)
	return err
}

func TestFileHandler_remove(t *testing.T) {
	fileHander := NewFileHandler(&fakeWritableFs{FS: os.DirFS(".")})
	ret, err := fileHander.HanldeFileOp(&FileOperation{Op: "remove", Path: "/LICENSE"})
	if err != nil {
		t.Fatal(err)
	}
	if data, ok := ret.(bool); ok {
		t.Log(data)
	} else {
		t.Error("type error", ret)
	}
}

func TestFileHandler_readtthumb(t *testing.T) {
	fileHander := NewFileHandler(os.DirFS("testdata/"))
	DefaultThumbnailer.Thumbnailers = append(DefaultThumbnailer.Thumbnailers, NewImageThumbnailer("cache"))
	ret, err := fileHander.HanldeFileOp(&FileOperation{Op: "read", Path: "test.png" + ThumbnailSuffix, Pos: 10, Len: 10})
	if err != nil {
		t.Fatal(err)
	}
	if data, ok := ret.([]byte); ok {
		t.Log(data)
	} else {
		t.Error("type error", ret)
	}
}
