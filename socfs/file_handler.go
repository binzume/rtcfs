package socfs

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"os"
	"path"
	"strings"
	"time"

	"golang.org/x/sync/semaphore"
)

type FileOperationRequest struct {
	Op   string `json:"op"`
	RID  any    `json:"rid,omitempty"`
	Path string `json:"path,omitempty"`
	Pos  int64  `json:"p,omitempty"`
	Len  int    `json:"l,omitempty"`
	Buf  []byte `json:"b,omitempty"`
}

type FileOperationResult struct {
	RID     any             `json:"rid,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
	Error   string          `json:"error,omitempty"`
	binData []byte
}

type FileEntry struct {
	Type        string `json:"type"`
	FileName    string `json:"name"`
	FileSize    int64  `json:"size"`
	UpdatedTime int64  `json:"updatedTime,omitempty"`
	CreatedTime int64  `json:"createdTime,omitempty"`
	Writable    bool   `json:"writable,omitempty"`

	Metadata map[string]any `json:"metadata,omitempty"`
}

func (f *FileEntry) Name() string {
	return f.FileName
}

func (f *FileEntry) Size() int64 {
	return f.FileSize
}

func (f *FileEntry) IsDir() bool {
	return f.Type == "directory"
}

func (f *FileEntry) Mode() fs.FileMode {
	var mode fs.FileMode = 1 << 8 // readable
	if f.Writable {
		mode |= 1 << 7
	}
	if f.IsDir() {
		mode |= fs.ModeDir | 1<<6
	}
	return mode
}

func (f *FileEntry) ModTime() time.Time {
	return time.UnixMilli(f.UpdatedTime)
}

func (f *FileEntry) Sys() any {
	return f
}

func (f *FileEntry) SetMetaData(key string, value any) {
	if f.Metadata == nil {
		f.Metadata = map[string]any{}
	}
	f.Metadata[key] = value
}

const BinaryMessageResponseType = 0
const ThumbnailSuffix = "#thumbnail.jpeg"

func (r *FileOperationRequest) ToBytes() []byte {
	b, err := json.Marshal(r)
	if err != nil {
		panic(err) // bug
	}
	return b
}

func (r *FileOperationResult) IsJSON() bool {
	return r.binData == nil
}

func (r *FileOperationResult) ToBytes() []byte {
	if !r.IsJSON() {
		var b []byte
		b = binary.LittleEndian.AppendUint32(b, uint32(BinaryMessageResponseType))
		b = binary.LittleEndian.AppendUint32(b, uint32(r.RID.(float64))) // TODO
		b = append(b, r.binData...)
		return b
	}
	b, err := json.Marshal(r)
	if err != nil {
		panic(err) // bug
	}
	return b
}

type FSServer struct {
	fsys FS
	sem  *semaphore.Weighted
}

func NewFSServer(fsys fs.FS, parallels int) *FSServer {
	return &FSServer{fsys: WrapFS(fsys), sem: semaphore.NewWeighted(int64(parallels))}
}

// well known types
var contentTypes = map[string]string{
	// video
	".mp4":  "video/mp4",
	".m4v":  "video/mp4",
	".f4v":  "video/mp4",
	".webm": "video/webm",
	".ogv":  "video/ogv",

	// image
	".jpeg": "image/jpeg",
	".jpg":  "image/jpeg",
	".gif":  "image/gif",
	".png":  "image/png",
	".bmp":  "image/bmp",
	".webp": "image/webp",

	// audio
	".aac": "audio/aac",
	".mp3": "audio/mp3",
	".ogg": "audio/ogg",
	".mid": "audio/midi",
}

func ContentTypeByPath(s string) string {
	ext := strings.ToLower(path.Ext(s))
	if typ, ok := contentTypes[ext]; ok {
		return typ
	}
	return mime.TypeByExtension(ext)
}

func NewFileEntry(info os.FileInfo, fswritable bool) *FileEntry {
	if ent, ok := info.(*FileEntry); ok {
		return ent
	}
	f := &FileEntry{
		FileName:    info.Name(),
		FileSize:    info.Size(),
		UpdatedTime: info.ModTime().UnixMilli(),
		Writable:    fswritable && info.Mode().Perm()&(1<<(uint(7))) != 0,
	}
	if info.IsDir() {
		f.Type = "directory"
	} else {
		f.Type = ContentTypeByPath(f.FileName)
	}
	if DefaultThumbnailer.Supported(f.Type) {
		f.SetMetaData("thumbnail", ThumbnailSuffix)
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

func errorToStr(err error) string {
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return "unexpected EOF"
	} else if errors.Is(err, io.EOF) {
		return "EOF"
	} else if errors.Is(err, fs.ErrNotExist) {
		return "noent"
	} else if errors.Is(err, fs.ErrClosed) {
		return "closed"
	} else if errors.Is(err, fs.ErrPermission) {
		return "permission error"
	}
	return fmt.Sprint(err)
}

func (h *FSServer) HandleMessage(ctx context.Context, data []byte, isjson bool, writer func(*FileOperationResult) error) error {
	if !isjson {
		rid := binary.LittleEndian.Uint32(data[4:8])
		writer(&FileOperationResult{RID: rid, Error: fmt.Sprint("TODO: support binary message")})
		return errors.New("not implemented")
	}

	var op FileOperationRequest
	err := json.Unmarshal(data, &op)
	if err != nil {
		return err
	}
	h.sem.Acquire(ctx, 1)
	go func() {
		defer h.sem.Release(1)

		ret, err := h.HanldeFileOp(&op)
		if err != nil {
			writer(&FileOperationResult{RID: op.RID, Error: errorToStr(err)})
		} else {
			if bindata, ok := ret.([]byte); ok {
				err = writer(&FileOperationResult{RID: op.RID, binData: bindata})
			} else {
				jsonData, _ := json.Marshal(ret)
				err = writer(&FileOperationResult{RID: op.RID, Data: jsonData})
			}
			if err != nil {
				_ = writer(&FileOperationResult{RID: op.RID, Error: errorToStr(err)})
			}
		}
	}()
	return nil
}

func (h *FSServer) readThumbnail(srcPath string, pos int64, len int) ([]byte, error) {
	typ := mime.TypeByExtension(path.Ext(srcPath))
	thumb, err := DefaultThumbnailer.GetThumbnail(context.TODO(), h.fsys, srcPath, typ, nil)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(thumb.Path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if pos > 0 {
		if _, err = f.Seek(pos, 0); err != nil {
			return nil, err
		}
	}

	buf := make([]byte, len)
	n, err := io.ReadFull(f, buf)
	if err != nil && (n == 0 || err != io.ErrUnexpectedEOF) {
		return nil, err
	}
	return buf[:n], nil
}

func (h *FSServer) HanldeFileOp(op *FileOperationRequest) (any, error) {
	switch op.Op {
	case "stat":
		stat, err := fs.Stat(h.fsys, fixPath(op.Path))
		if err != nil {
			return nil, err
		}
		return NewFileEntry(stat, h.fsys.Capability().Write), nil
	case "files":
		entries, err := fs.ReadDir(h.fsys, fixPath(op.Path))
		if err != nil {
			return nil, err
		}
		files := []*FileEntry{}
		if op.Pos >= int64(len(entries)) {
			return files, nil
		}
		end := int64(len(entries))
		if op.Len > 0 && op.Pos+int64(op.Len) < end {
			end = op.Pos + int64(op.Len)
		}
		entries = entries[op.Pos:end]
		for _, ent := range entries {
			info, _ := ent.Info()
			files = append(files, NewFileEntry(info, h.fsys.Capability().Write))
		}
		return files, nil
	case "read":
		if strings.HasSuffix(op.Path, ThumbnailSuffix) {
			return h.readThumbnail(fixPath(strings.TrimSuffix(op.Path, ThumbnailSuffix)), op.Pos, op.Len)
		}
		f, err := h.fsys.Open(fixPath(op.Path))
		if err != nil {
			return nil, err
		}
		defer f.Close()
		buf := make([]byte, op.Len)
		if f, ok := f.(io.ReaderAt); ok {
			n, err := f.ReadAt(buf, op.Pos)
			if err != nil && (n == 0 || err != io.ErrUnexpectedEOF && err != io.EOF) {
				return nil, err
			}
			return buf[:n], nil
		}
	case "write":
		f, err := h.fsys.OpenWriter(fixPath(op.Path), os.O_CREATE|os.O_WRONLY)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		if f, ok := f.(io.WriterAt); ok {
			_, err := f.WriteAt(op.Buf, op.Pos)
			if err != nil {
				return nil, err
			}
			return nil, nil
		}
	case "truncate":
		f, err := h.fsys.OpenWriter(fixPath(op.Path), os.O_CREATE|os.O_WRONLY)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		if f, ok := f.(interface{ Truncate(int64) error }); ok {
			return nil, f.Truncate(op.Pos)
		} else {
			return nil, errors.New("unsupported operation")
		}
	case "remove":
		err := h.fsys.Remove(fixPath(op.Path))
		return err == nil, err
	}
	return nil, errors.New("unsupported operation")
}
