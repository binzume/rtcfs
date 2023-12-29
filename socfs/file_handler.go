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
	"sort"
	"strings"
	"time"

	"golang.org/x/sync/semaphore"
)

type FileOperationRequest struct {
	Op    string `json:"op"`
	RID   any    `json:"rid,omitempty"`
	Path  string `json:"path,omitempty"`
	Path2 string `json:"path2,omitempty"`
	Pos   int64  `json:"p,omitempty"`
	Len   int    `json:"l,omitempty"`
	Buf   []byte `json:"b,omitempty"`

	Options map[string]string `json:"options,omitempty"`
}

type FileOperationResult struct {
	RID   any             `json:"rid,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
	Error string          `json:"error,omitempty"`
	Buf   []byte          `json:"b,omitempty"`
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
	return r.Buf == nil
}

func (r *FileOperationResult) ToBytes() []byte {
	if !r.IsJSON() {
		var b []byte
		b = binary.LittleEndian.AppendUint32(b, uint32(BinaryMessageResponseType))
		b = binary.LittleEndian.AppendUint32(b, uint32(r.RID.(float64))) // TODO
		b = append(b, r.Buf...)
		return b
	}
	b, err := json.Marshal(r)
	if err != nil {
		panic(err) // bug
	}
	return b
}

type FSServer struct {
	fsys *WrappedFS
	sem  *semaphore.Weighted
}

func NewFSServer(fsys fs.FS, parallels int) *FSServer {
	return &FSServer{fsys: WrapFS(fsys), sem: semaphore.NewWeighted(int64(parallels))}
}

func (s *FSServer) FSCaps() *FSCapability {
	return s.fsys.Capability()
}

// well known types
var ContentTypes = map[string]string{
	// video
	".mp4":  "video/mp4",
	".m4v":  "video/mp4",
	".f4v":  "video/mp4",
	".mov":  "video/mp4",
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
	if typ, ok := ContentTypes[ext]; ok {
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
	} else if errors.Is(err, fs.ErrInvalid) {
		return "invalid argument"
	}
	return fmt.Sprint(err)
}

func (h *FSServer) ErrorReply(ctx context.Context, data []byte, isjson bool, writer func(*FileOperationResult) error, msg string) error {
	var rid any
	if !isjson {
		rid = binary.LittleEndian.Uint32(data[4:8])
	} else {
		var op FileOperationRequest
		err := json.Unmarshal(data, &op)
		if err != nil {
			return err
		}
		rid = op.RID
	}
	return writer(&FileOperationResult{RID: rid, Error: msg})
}

func (h *FSServer) HandleMessage(ctx context.Context, data []byte, isjson bool, writer func(*FileOperationResult) error) error {
	if !isjson {
		h.ErrorReply(ctx, data, isjson, writer, "TODO: support binary message")
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
				err = writer(&FileOperationResult{RID: op.RID, Buf: bindata})
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

// retrun true if a < b
func compareString(a, b string) bool {
	la := len(a)
	lb := len(b)
	pa := 0
	pb := 0
	for pa < la && pb < lb {
		ca := a[pa]
		cb := b[pb]
		if ca >= '0' && ca <= '9' && cb >= '0' && cb <= '9' {
			na, la := readNum(a[pa:])
			nb, lb := readNum(b[pb:])
			if na != nb {
				return na < nb
			}
			pa += la
			pb += lb
			continue
		}
		if ca >= 'a' && ca <= 'z' {
			ca -= 'a' - 'A'
		}
		if cb >= 'a' && cb <= 'z' {
			cb -= 'a' - 'A'
		}
		if ca != cb {
			return ca < cb
		}
		pa++
		pb++
	}
	return la < lb
}

func readNum(s string) (int, int) {
	v := 0
	l := len(s)
	p := 0
	for ; p < l; p++ {
		c := s[p]
		if c < '0' || c > '9' {
			break
		}
		v = v*10 + int(c-'0')
	}
	return v, p
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
		// TODO: OpenDir(), ReadDirN()
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
		infos := make([]fs.FileInfo, len(entries))
		for i, ent := range entries {
			infos[i], _ = ent.Info()
		}
		if op.Options != nil && op.Options["sort"] != "" {
			sortField := op.Options["sort"]
			asc := true
			if sortField[0] == '-' {
				asc = false
				sortField = sortField[1:]
			}
			if sortField == "updatedTime" {
				sort.Slice(infos, func(i, j int) bool {
					return infos[i].ModTime().Before(infos[j].ModTime()) == asc
				})
			} else if sortField == "size" {
				sort.Slice(infos, func(i, j int) bool {
					return infos[i].Size() < infos[j].Size() == asc
				})
			} else if sortField == "name" {
				sort.Slice(infos, func(i, j int) bool { return compareString(infos[i].Name(), infos[j].Name()) == asc })
			} else if sortField == "type" {
				sort.Slice(infos, func(i, j int) bool { return path.Ext(infos[i].Name()) < path.Ext(infos[j].Name()) == asc })
			}
		}
		infos = infos[op.Pos:end]
		for _, info := range infos {
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
		return nil, h.fsys.Truncate(fixPath(op.Path), op.Pos)
	case "mkdir":
		return nil, h.fsys.Mkdir(fixPath(op.Path), fs.ModePerm)
	case "rename":
		return nil, h.fsys.Rename(fixPath(op.Path), fixPath(op.Path2))
	case "remove":
		err := h.fsys.Remove(fixPath(op.Path))
		return err == nil, err
	}
	return nil, errors.New("unsupported operation")
}
