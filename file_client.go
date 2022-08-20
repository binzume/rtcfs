package main

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
			return res, fs.ErrNotExist
		default:
			return res, errors.New(res.Error)
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
		res.binData = data[8:]
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

type OpenDirFS interface {
	OpenDir(name string) (fs.ReadDirFile, error)
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

type clientDirEnt struct {
	*FileEntry
}

func (f *clientDirEnt) Type() fs.FileMode {
	return f.Mode()
}

func (f *clientDirEnt) Info() (fs.FileInfo, error) {
	return f.FileEntry, nil
}

type clientFile struct {
	c    *FSClient
	name string
	pos  int64
}

func (f *clientFile) Stat() (fs.FileInfo, error) {
	return f.c.Stat(f.name)
}

func (f *clientFile) Read(b []byte) (int, error) {
	sz := len(b)
	if sz > f.c.MaxReadSize {
		sz = f.c.MaxReadSize
	}
	res, err := f.c.request(&FileOperationRequest{Op: "read", Path: f.name, Pos: f.pos, Len: sz})
	l := copy(b, res.binData)
	f.pos += int64(l)
	if err == nil && l < sz {
		err = io.EOF
	}
	return l, err
}

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
