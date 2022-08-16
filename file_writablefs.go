package main

import (
	"fmt"
	"io"
	"io/fs"
)

type FSCapability struct {
	Read   bool `json:"read"`
	Write  bool `json:"write"`
	Create bool `json:"create"`
	Remove bool `json:"remove"`
}

type FS interface {
	fs.StatFS
	Capability() FSCapability
	OpenWriter(path string) (io.WriteCloser, error)
	Create(path string) (io.WriteCloser, error)
	Remove(path string) error
}

type wrappedFS struct {
	fs.FS
	cap FSCapability
}

func (w *wrappedFS) Capability() FSCapability {
	return w.cap
}

func (w *wrappedFS) OpenWriter(path string) (io.WriteCloser, error) {
	if !w.cap.Write {
		return nil, fmt.Errorf("not supported operation")
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
