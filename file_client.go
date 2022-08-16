package main

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"sync"
)

// FSClient implements fs.FS
type FSClient struct {
	sendFunc    func(req *FileOperationRequest) error
	reqCount    uint32
	MaxReadSize int
	wait        map[uint32]chan *FileOperationResult
	locker      sync.Mutex
}

func NewFSClient(sendFunc func(req *FileOperationRequest) error) *FSClient {
	return &FSClient{sendFunc: sendFunc, wait: map[uint32]chan *FileOperationResult{}, MaxReadSize: 48000}
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
	res := <-resCh
	if res == nil {
		return nil, os.ErrClosed
	}
	if res.Error != "" {
		// TODO: more errors
		switch res.Error {
		case "unexpected EOF":
			return nil, io.ErrUnexpectedEOF
		case "EOF":
			return nil, io.EOF
		case "file does not exist":
			return nil, fs.ErrNotExist
		default:
			return nil, errors.New(res.Error)
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

func (c *FSClient) ReadDirRange(name string, pos, limit int) ([]fs.DirEntry, error) {
	res, err := c.request(&FileOperationRequest{Op: "files", Path: name, Pos: int64(pos), Len: limit})
	if err != nil {
		return nil, err
	}
	var result []*FileEntry
	json.Unmarshal(res.Data, &result)
	var entries []fs.DirEntry
	for _, f := range result {
		entries = append(entries, &clientDirEnt{FileEntry: f})
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
	return f, nil
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
	if err != nil {
		return 0, err
	}
	l := copy(b, res.binData)
	f.pos += int64(l)
	return l, nil
}

func (f *clientFile) Close() error {
	return nil
}

// fs.ReadDirFile
func (f *clientFile) ReadDir(n int) ([]fs.DirEntry, error) {
	entries, err := f.c.ReadDirRange(f.name, int(f.pos), n)
	f.pos += int64(len(entries))
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
