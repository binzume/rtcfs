package main

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"sync"
)

type FileClient struct {
	sendFunc func(req *FileOperation) error
	seq      uint32
	locker   sync.Mutex
	wait     map[uint32]chan *FileOperationResult
}

func NewFileClient(sendFunc func(res *FileOperation) error) *FileClient {
	return &FileClient{sendFunc: sendFunc, wait: map[uint32]chan *FileOperationResult{}}
}

func (c *FileClient) request(req *FileOperation) (*FileOperationResult, error) {
	resCh := make(chan *FileOperationResult)

	c.locker.Lock()
	c.seq++
	c.wait[c.seq] = resCh
	req.RID = c.seq
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
		return nil, errors.New(res.Error)
	}
	return res, nil
}

func (c *FileClient) HandleMessage(data []byte, isstr bool) error {
	var res FileOperationResult
	if isstr {
		if err := json.Unmarshal(data, &res); err != nil {
			return err
		}
	} else {
		if binary.LittleEndian.Uint32(data) != BinaryMessageResponseType {
			return errors.New("invalid binary msssage type")
		}
		res.RID = float64(binary.LittleEndian.Uint32(data[4:]))
		res.Data = data[8:]
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
	c    *FileClient
	name string
	pos  int64
}

func (f *clientFile) Stat() (fs.FileInfo, error) {
	res, err := f.c.request(&FileOperation{Op: "stat", Path: f.name})
	if err != nil {
		return nil, err
	}
	// TODO: json.RawMessage...
	bytes, _ := json.Marshal(res.Data)
	var result FileEntry
	json.Unmarshal(bytes, &result)
	return &result, nil
}

func (f *clientFile) Read(b []byte) (int, error) {
	sz := len(b)
	if sz > 48000 {
		sz = 48000
	}
	res, err := f.c.request(&FileOperation{Op: "read", Path: f.name, Pos: f.pos, Len: sz})
	if err != nil {
		return 0, err
	}
	l := copy(b, res.Data.([]byte))
	f.pos += int64(l)
	return l, nil
}
func (f *clientFile) Close() error {
	return nil
}

func (c *FileClient) Open(name string) (fs.File, error) {
	return &clientFile{c: c, name: name}, nil
}

func (c *FileClient) ReadDir(name string) ([]fs.DirEntry, error) {
	res, err := c.request(&FileOperation{Op: "files", Path: name, Len: 500})
	if err != nil {
		return nil, err
	}
	// TODO: json.RawMessage...
	bytes, _ := json.Marshal(res.Data)
	var result []*FileEntry
	json.Unmarshal(bytes, &result)
	var entries []fs.DirEntry
	for _, f := range result {
		entries = append(entries, &clientDirEnt{FileEntry: f})
	}
	return entries, nil
}
