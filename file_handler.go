package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"os"
	"path"
	"strings"
)

type FileOperation struct {
	Op   string `json:"op"`
	RID  any    `json:"rid,omitempty"`
	Path string `json:"path,omitempty"`
	Pos  int64  `json:"p,omitempty"`
	Len  int    `json:"l,omitempty"`
	Buf  []byte `json:"b,omitempty"`
}

type FileOperationResult struct {
	RID   any    `json:"rid,omitempty"`
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

type FileEntry struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	UpdatedTime int64  `json:"updatedTime"`

	Metadata map[string]interface{} `json:"metadata"`
}

type RemoveFS interface {
	Remove(path string) error
}

const BinaryMessageResponseType = 0
const ThumbnailSuffix = "#thumbnail.jpeg"

func (r *FileOperationResult) IsBinary() bool {
	_, ok := r.Data.([]byte)
	return ok
}

func (r *FileOperationResult) ToBytes() ([]byte, error) {
	if data, ok := r.Data.([]byte); ok {
		var b []byte
		b = binary.LittleEndian.AppendUint32(b, uint32(BinaryMessageResponseType))
		b = binary.LittleEndian.AppendUint32(b, uint32(r.RID.(float64))) // TODO
		b = append(b, data...)
		return b, nil
	}
	return json.Marshal(r)
}

type FileHandler struct {
	Fs fs.FS
}

func NewFileHandler(f fs.FS) *FileHandler {
	return &FileHandler{Fs: f}
}

func NewFileEntry(info os.FileInfo) *FileEntry {
	f := &FileEntry{Name: info.Name(), Size: info.Size(), UpdatedTime: info.ModTime().UnixMilli()}
	if info.IsDir() {
		f.Type = "directory"
	} else {
		f.Type = mime.TypeByExtension(path.Ext(f.Name))
	}
	if DefaultThumbnailer.Supported(f.Type) {
		f.Metadata = map[string]interface{}{
			"thumbnail": ThumbnailSuffix,
		}
	}
	return f
}

func fixPath(path string) string {
	path = strings.TrimPrefix(path, "/")
	if path == "" {
		return "."
	}
	return path
}

func (h *FileHandler) HandleMessage(data []byte, isString bool) *FileOperationResult {
	var result *FileOperationResult
	if isString {
		var op FileOperation
		_ = json.Unmarshal(data, &op)
		data, err := h.HanldeFileOp(&op)
		if err != nil {
			result = &FileOperationResult{RID: op.RID, Data: data, Error: fmt.Sprint(err)}
		} else if data != nil {
			result = &FileOperationResult{RID: op.RID, Data: data}
		}
	} else {
		rid := binary.LittleEndian.Uint32(data[4:8])
		result = &FileOperationResult{RID: rid, Error: fmt.Sprint("TODO: support binary message")}
	}
	return result
}

func (h *FileHandler) HanldeFileOp(op *FileOperation) (any, error) {
	switch op.Op {
	case "stat":
		stat, err := fs.Stat(h.Fs, fixPath(op.Path))
		if err != nil {
			return nil, err
		}
		return NewFileEntry(stat), nil
	case "files":
		entries, err := fs.ReadDir(h.Fs, fixPath(op.Path))
		if err != nil {
			return nil, err
		}
		var files []*FileEntry
		for _, ent := range entries {
			info, _ := ent.Info()
			files = append(files, NewFileEntry(info))
		}
		return files, nil
	case "read":
		if strings.HasSuffix(op.Path, ThumbnailSuffix) {
			srcPath := strings.TrimSuffix(op.Path, ThumbnailSuffix)
			typ := mime.TypeByExtension(path.Ext(srcPath))
			log.Println(fixPath(srcPath), typ)
			thumb := DefaultThumbnailer.GetThumbnail(context.TODO(), h.Fs, fixPath(srcPath), typ, nil)
			if thumb == nil {
				return nil, fmt.Errorf("not found")
			}
			f, err := os.Open(thumb.Path)
			if err != nil {
				return nil, err
			}
			defer f.Close()
			f.Seek(op.Pos, 0)
			buf := make([]byte, op.Len)
			io.ReadFull(f, buf)
			if err != nil {
				return nil, err
			}
			return buf, nil
		}
		f, err := h.Fs.Open(fixPath(op.Path))
		if err != nil {
			return nil, err
		}
		defer f.Close()
		f.(io.ReadSeeker).Seek(op.Pos, 0) // TODO
		buf := make([]byte, op.Len)
		io.ReadFull(f, buf)
		if err != nil {
			return nil, err
		}
		return buf, nil
	case "remove":
		if fs, ok := h.Fs.(RemoveFS); ok {
			return fs.Remove(fixPath(op.Path)) == nil, nil
		}
		return nil, fmt.Errorf("Unsupported operation")
	}
	return nil, nil
}
