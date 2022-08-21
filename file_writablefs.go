package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
)

type FSCapability struct {
	Read   bool `json:"read"`
	Write  bool `json:"write"`
	Create bool `json:"create"`
	Remove bool `json:"remove"`
}

type FS interface {
	fs.FS
	Capability() *FSCapability
	OpenWriter(path string) (io.WriteCloser, error)
	Create(path string) (io.WriteCloser, error)
	Remove(path string) error
}

type wrappedFS struct {
	fs.FS
	cap FSCapability
}

func (w *wrappedFS) Capability() *FSCapability {
	return &w.cap
}

func (w *wrappedFS) OpenWriter(path string) (io.WriteCloser, error) {
	if !w.cap.Write {
		return nil, fs.ErrPermission
	}
	return w.FS.(interface {
		OpenWriter(path string) (io.WriteCloser, error)
	}).OpenWriter(path)
}

func (w *wrappedFS) Remove(path string) error {
	if !w.cap.Remove {
		return fmt.Errorf("not supported operation")
	}
	return w.FS.(interface{ Remove(path string) error }).Remove(path)
}

func (w *wrappedFS) Create(path string) (io.WriteCloser, error) {
	if !w.cap.Create {
		return nil, fmt.Errorf("not supported operation")
	}
	return w.FS.(interface {
		Create(path string) (io.WriteCloser, error)
	}).Create(path)
}

func (w *wrappedFS) Stat(name string) (fs.FileInfo, error) {
	return fs.Stat(w.FS, name)
}

func WrapFS(fsys fs.FS) FS {
	if fsys, ok := fsys.(FS); ok {
		return fsys
	}
	return &wrappedFS{FS: fsys, cap: Capability(fsys)}
}

func Capability(fsys fs.FS) FSCapability {
	cap := FSCapability{}
	if fsys == nil {
		return cap
	}

	cap.Read = true
	_, cap.Write = fsys.(interface {
		OpenWriter(path string) (io.WriteCloser, error)
	})
	_, cap.Remove = fsys.(interface {
		Remove(path string) error
	})
	_, cap.Create = fsys.(interface {
		Create(path string) (io.WriteCloser, error)
	})

	return cap
}

type writableFS struct {
	fs.FS
	Path string
	cap  *FSCapability
}

func NewWritableDirFS(dir string) FS {
	return &writableFS{FS: os.DirFS(dir), Path: dir, cap: &FSCapability{true, true, true, true}}
}

func (w *writableFS) Capability() *FSCapability {
	return w.cap
}

func (fsys *writableFS) Create(name string) (io.WriteCloser, error) {
	if !fsys.cap.Create {
		return nil, fs.ErrPermission
	}
	return os.Create(path.Join(fsys.Path, name))
}

func (fsys *writableFS) Remove(name string) error {
	if !fsys.cap.Remove {
		return fs.ErrPermission
	}
	return os.Remove(path.Join(fsys.Path, name))
}

func (fsys *writableFS) OpenWriter(name string) (io.WriteCloser, error) {
	if !fsys.cap.Write {
		return nil, fs.ErrPermission
	}
	return os.OpenFile(path.Join(fsys.Path, name), os.O_RDWR|os.O_CREATE, fs.ModePerm)
}
