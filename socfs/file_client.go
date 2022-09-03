package socfs

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"sync"
	"time"
)

// FSClient implements fs.FS
type FSClient struct {
	sendFunc    func(req *FileOperationRequest) error
	reqCount    uint32
	wait        map[uint32]chan *FileOperationResult
	locker      sync.Mutex
	MaxReadSize int
	Timeout     time.Duration
}

func NewFSClient(sendFunc func(req *FileOperationRequest) error) *FSClient {
	return &FSClient{sendFunc: sendFunc, wait: map[uint32]chan *FileOperationResult{}, MaxReadSize: 65000, Timeout: 30 * time.Second}
}

func (c *FSClient) request(req *FileOperationRequest) (*FileOperationResult, error) {
	resCh := make(chan *FileOperationResult, 1)

	c.locker.Lock()
	c.reqCount++
	c.wait[c.reqCount] = resCh
	req.RID = c.reqCount
	c.locker.Unlock()

	err := c.sendFunc(req)
	if err != nil {
		return nil, err
	}
	var res *FileOperationResult
	select {
	case <-time.After(c.Timeout):
		return nil, errors.New("timeout")
	case res = <-resCh:
		if res == nil {
			return nil, os.ErrClosed
		}
	}
	if res.Error != "" {
		// TODO: more errors
		switch res.Error {
		case "unexpected EOF":
			return res, io.ErrUnexpectedEOF
		case "EOF":
			return res, io.EOF
		case "noent":
			return res, &fs.PathError{Op: req.Op, Path: req.Path, Err: fs.ErrNotExist}
		case "closed":
			return res, &fs.PathError{Op: req.Op, Path: req.Path, Err: fs.ErrClosed}
		case "permission error":
			return res, &fs.PathError{Op: req.Op, Path: req.Path, Err: fs.ErrPermission}
		case "invalid argument":
			return res, &fs.PathError{Op: req.Op, Path: req.Path, Err: fs.ErrInvalid}
		default:
			return res, &fs.PathError{Op: req.Op, Path: req.Path, Err: errors.New(res.Error)}
		}
	}
	return res, nil
}

func (c *FSClient) HandleMessage(data []byte, isjson bool) error {
	var res FileOperationResult
	if isjson {
		if err := json.Unmarshal(data, &res); err != nil {
			return err
		}
	} else {
		if binary.LittleEndian.Uint32(data) != BinaryMessageResponseType {
			return errors.New("invalid binary msssage type")
		}
		res.RID = float64(binary.LittleEndian.Uint32(data[4:]))
		res.Buf = data[8:]
	}
	rid := uint32(res.RID.(float64))
	c.locker.Lock()
	if ch, ok := c.wait[rid]; ok {
		ch <- &res
		delete(c.wait, rid)
	}
	c.locker.Unlock()
	return nil
}

// fs.FS
func (c *FSClient) Open(name string) (fs.File, error) {
	return &clientFile{c: c, name: name}, nil
}

// fs.StatFS
func (c *FSClient) Stat(name string) (fs.FileInfo, error) {
	res, err := c.request(&FileOperationRequest{Op: "stat", Path: name})
	if err != nil {
		return nil, err
	}
	var result FileEntry
	json.Unmarshal(res.Data, &result)
	return &result, nil
}

// fs.ReadDirFS
func (c *FSClient) ReadDir(name string) ([]fs.DirEntry, error) {
	return c.ReadDirRange(name, 0, -1)
}

func (c *FSClient) OpenDir(name string) (fs.ReadDirFile, error) {
	return &clientFile{c: c, name: name}, nil
}

func (c *FSClient) ReadDirRange(name string, pos, limit int) ([]fs.DirEntry, error) {
	var entries []fs.DirEntry
	if limit < 0 {
		limit = 65536
	}

	for {
		n := limit - len(entries)
		if n <= 0 {
			return entries, nil
		}
		if n > 200 {
			n = 200
		}
		res, err := c.request(&FileOperationRequest{Op: "files", Path: name, Pos: int64(pos), Len: n})
		if err != nil {
			return entries, err
		}
		var result []*FileEntry
		json.Unmarshal(res.Data, &result)
		for _, f := range result {
			entries = append(entries, &clientDirEnt{FileEntry: f})
		}
		if len(result) != n {
			break // io.EOF
		}
	}

	return entries, nil
}

func (c *FSClient) Create(name string) (io.WriteCloser, error) {
	err := c.Truncate(name, 0)
	if err != nil {
		return nil, err
	}
	return &clientFile{c: c, name: name}, nil
}

func (c *FSClient) Rename(name string, newName string) error {
	_, err := c.request(&FileOperationRequest{Op: "rename", Path: name, Path2: newName})
	return err
}

func (c *FSClient) Mkdir(name string, mode fs.FileMode) error {
	_, err := c.request(&FileOperationRequest{Op: "mkdir", Path: name})
	return err
}

func (c *FSClient) Remove(name string) error {
	_, err := c.request(&FileOperationRequest{Op: "remove", Path: name})
	return err
}

func (c *FSClient) Truncate(name string, size int64) error {
	_, err := c.request(&FileOperationRequest{Op: "truncate", Path: name, Pos: size})
	return err
}

func (c *FSClient) OpenWriter(name string, flag int) (io.WriteCloser, error) {
	var err error
	if flag&os.O_TRUNC != 0 {
		err = c.Truncate(name, 0)
	}
	return &clientFile{c: c, name: name}, err
}

type clientDirEnt struct {
	*FileEntry
}

func (f *clientDirEnt) Type() fs.FileMode {
	return f.Mode().Type()
}

func (f *clientDirEnt) Info() (fs.FileInfo, error) {
	return f.FileEntry, nil
}

type clientFile struct {
	c    *FSClient
	name string
	pos  int64
}

// fs.File
func (f *clientFile) Stat() (fs.FileInfo, error) {
	return f.c.Stat(f.name)
}

// fs.File, io.Reader
func (f *clientFile) Read(b []byte) (int, error) {
	sz := len(b)
	if sz > f.c.MaxReadSize {
		sz = f.c.MaxReadSize
	}
	res, err := f.c.request(&FileOperationRequest{Op: "read", Path: f.name, Pos: f.pos, Len: sz})
	l := copy(b, res.Buf)
	f.pos += int64(l)
	if err == nil && l < sz {
		err = io.EOF
	}
	return l, err
}

// io.ReaderAt
func (f *clientFile) ReadAt(b []byte, off int64) (int, error) {
	f.pos = off
	read := 0
	for read < len(b) {
		n, err := f.Read(b[read:])
		read += n
		if err != nil {
			return read, err
		}
	}
	return read, nil
}

// io.Writer
func (f *clientFile) Write(b []byte) (int, error) {
	return f.WriteAt(b, f.pos)
}

// io.WriterAt
func (f *clientFile) WriteAt(b []byte, off int64) (int, error) {
	wrote := 0
	for len(b) > 0 {
		l := len(b)
		if l > f.c.MaxReadSize {
			l = f.c.MaxReadSize
		}
		_, err := f.c.request(&FileOperationRequest{Op: "write", Path: f.name, Pos: off, Buf: b[:l]})
		if err != nil {
			return wrote, err
		}
		wrote += l
		off += int64(l)
		b = b[l:]
	}
	f.pos = off
	return wrote, nil
}

func (f *clientFile) Truncate(size int64) error {
	return f.c.Truncate(f.name, size)
}

// fs.File
func (f *clientFile) Close() error {
	return nil
}

// fs.ReadDirFile
func (f *clientFile) ReadDir(n int) ([]fs.DirEntry, error) {
	entries, err := f.c.ReadDirRange(f.name, int(f.pos), n)
	f.pos += int64(len(entries))
	if err == nil && len(entries) < n {
		err = io.EOF
	}
	return entries, err
}

// Abort all requests
func (c *FSClient) Abort() error {
	c.locker.Lock()
	defer c.locker.Unlock()
	for _, ch := range c.wait {
		close(ch)
	}
	c.wait = map[uint32]chan *FileOperationResult{}
	return nil
}
