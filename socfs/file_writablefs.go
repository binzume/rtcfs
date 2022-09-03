package socfs

import (
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

type OpenWriterFS interface {
	OpenWriter(name string, flag int) (io.WriteCloser, error)
}

type RemoveFS interface {
	Remove(name string) error
}

type RenameFS interface {
	Rename(name string, newName string) error
}

type MkdirFS interface {
	Mkdir(name string, mode fs.FileMode) error
}

type OpenDirFS interface {
	OpenDir(name string) (fs.ReadDirFile, error)
}

type TruncateFS interface {
	Truncate(name string, size int64) error
}

type CreateFS interface {
	Create(path string) (io.WriteCloser, error)
}

type WrappedFS struct {
	fs.FS
	openWriterFS OpenWriterFS
	createFS     CreateFS
	truncateFS   TruncateFS
	removeFS     RemoveFS
	renameFS     RenameFS
	mkdirFS      MkdirFS
}

func WrapFS(fsys fs.FS) *WrappedFS {
	if fsys, ok := fsys.(*WrappedFS); ok {
		return fsys
	}
	w := &WrappedFS{FS: fsys}
	w.openWriterFS, _ = fsys.(OpenWriterFS)
	w.createFS, _ = fsys.(CreateFS)
	w.truncateFS, _ = fsys.(TruncateFS)
	w.removeFS, _ = fsys.(RemoveFS)
	w.renameFS, _ = fsys.(RenameFS)
	w.mkdirFS, _ = fsys.(MkdirFS)
	return w
}

func (w *WrappedFS) Capability() *FSCapability {
	return &FSCapability{
		Read:   true,
		Write:  w.openWriterFS != nil,
		Create: w.createFS != nil || w.openWriterFS != nil,
		Remove: w.removeFS != nil,
	}
}

func (w *WrappedFS) ReadOnly() *WrappedFS {
	w.openWriterFS = nil
	w.createFS = nil
	w.removeFS = nil
	w.renameFS = nil
	w.mkdirFS = nil
	return w
}

func (w *WrappedFS) OpenWriter(name string, flag int) (io.WriteCloser, error) {
	if w.openWriterFS == nil {
		return nil, fs.ErrPermission
	}
	return w.openWriterFS.OpenWriter(name, flag)
}

func (w *WrappedFS) Create(name string) (io.WriteCloser, error) {
	if w.createFS != nil {
		return w.createFS.Create(name)
	}
	if w.openWriterFS != nil {
		return w.openWriterFS.OpenWriter(name, os.O_RDWR|os.O_CREATE|os.O_TRUNC)
	}
	return nil, fs.ErrPermission
}

func (w *WrappedFS) Truncate(name string, size int64) error {
	if w.truncateFS != nil {
		return w.truncateFS.Truncate(name, size)
	}
	if w.openWriterFS != nil {
		f, err := w.openWriterFS.OpenWriter(name, os.O_RDWR|os.O_CREATE)
		if err != nil {
			return err
		}
		defer f.Close()
		if trunc, ok := f.(interface{ Truncate(int64) error }); ok {
			return trunc.Truncate(size)
		}
	}
	return fs.ErrPermission
}

func (w *WrappedFS) Remove(name string) error {
	if w.removeFS != nil {
		return w.removeFS.Remove(name)
	}
	return fs.ErrPermission
}

func (w *WrappedFS) Rename(name, newName string) error {
	if w.renameFS != nil {
		return w.renameFS.Rename(name, newName)
	}
	return fs.ErrPermission
}

func (w *WrappedFS) Mkdir(path string, mode fs.FileMode) error {
	if w.mkdirFS != nil {
		return w.mkdirFS.Mkdir(path, mode)
	}
	return fs.ErrPermission
}

func (w *WrappedFS) Stat(name string) (fs.FileInfo, error) {
	return fs.Stat(w.FS, name)
}

func (w *WrappedFS) ReadDir(name string) ([]fs.DirEntry, error) {
	return fs.ReadDir(w.FS, name)
}

type writableDirFS struct {
	fs.StatFS
	path string
}

func NewWritableDirFS(path string) *writableDirFS {
	return &writableDirFS{StatFS: os.DirFS(path).(fs.StatFS), path: path}
}

func (fsys *writableDirFS) OpenWriter(name string, flag int) (io.WriteCloser, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	return os.OpenFile(path.Join(fsys.path, name), flag, fs.ModePerm)
}

func (fsys *writableDirFS) Create(name string) (io.WriteCloser, error) {
	return fsys.OpenWriter(name, os.O_RDWR|os.O_CREATE|os.O_TRUNC)
}

func (fsys *writableDirFS) Remove(name string) error {
	if !fs.ValidPath(name) {
		return &fs.PathError{Op: "remove", Path: name, Err: fs.ErrInvalid}
	}
	return os.Remove(path.Join(fsys.path, name))
}

func (fsys *writableDirFS) Rename(name, newName string) error {
	if !fs.ValidPath(name) || !fs.ValidPath(newName) {
		return &fs.PathError{Op: "rename", Path: name, Err: fs.ErrInvalid}
	}
	return os.Rename(path.Join(fsys.path, name), path.Join(fsys.path, newName))
}

func (fsys *writableDirFS) Mkdir(name string, mode fs.FileMode) error {
	if !fs.ValidPath(name) {
		return &fs.PathError{Op: "mkdir", Path: name, Err: fs.ErrInvalid}
	}
	return os.Mkdir(path.Join(fsys.path, name), mode)
}
