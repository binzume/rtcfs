package socfs

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io/fs"
	"log"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path"
	"strings"
	"sync"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"

	"github.com/nfnt/resize"
)

type Thumbnail struct {
	Type string
	Path string
}

type Thumbnailer interface {
	Supported(typ string) bool
	GetThumbnail(ctx context.Context, f fs.FS, path, typ string, opt any) (*Thumbnail, error)
}

var ErrNotSupported = errors.New("Not supported format")

var DefaultThumbnailer = ThumbnailerGroup{}

const ThumbnailWidth = 160

type ThumbnailerGroup struct {
	Thumbnailers []Thumbnailer
}

func (g ThumbnailerGroup) Supported(typ string) bool {
	for _, t := range g.Thumbnailers {
		if t.Supported(typ) {
			return true
		}
	}
	return false
}

func (g ThumbnailerGroup) GetThumbnail(ctx context.Context, f fs.FS, path, typ string, opt any) (*Thumbnail, error) {
	for _, t := range g.Thumbnailers {
		if t.Supported(typ) {
			thumb, err := t.GetThumbnail(ctx, f, path, typ, opt)
			if err != ErrNotSupported {
				return thumb, err // succeeded or unexpected error
			}
		}
	}
	return nil, ErrNotSupported
}

func (g *ThumbnailerGroup) Register(t Thumbnailer) {
	g.Thumbnailers = append(g.Thumbnailers, t)
}

type promise[T any] struct {
	complete chan struct{}
	value    T
	err      error
}

func NewPromise[T any]() *promise[T] {
	return &promise[T]{complete: make(chan struct{})}
}

func (c *promise[T]) Resolve(value T, err error) {
	c.value = value
	c.err = err
	close(c.complete)
}

func (c *promise[T]) Wait(ctx context.Context) (T, error) {
	select {
	case <-c.complete:
		return c.value, c.err
	case <-ctx.Done():
		return c.value, ctx.Err()
	}
}

type CachedThumbnailer struct {
	CacheDir      string
	SupportedFunc func(typ string) bool
	GenerateFunc  func(ctx context.Context, f fs.FS, src, dst string) error
	locker        sync.Mutex
	generating    map[string]*promise[*Thumbnail] // TODO: cache state
}

func (t *CachedThumbnailer) Supported(typ string) bool {
	return t.SupportedFunc(typ)
}

func (t *CachedThumbnailer) GetThumbnail(ctx context.Context, f fs.FS, src, typ string, opt any) (*Thumbnail, error) {
	sum := sha1.Sum([]byte(src))
	cacheID := hex.EncodeToString(sum[:])
	cachePath := path.Join(t.CacheDir, cacheID+".jpeg")
	thumb := &Thumbnail{Path: cachePath, Type: "image/jpeg"}

	if _, err := os.Stat(cachePath); err == nil {
		return thumb, nil
	}

	for task, exists := t.prepare(cacheID); exists; task, exists = t.prepare(cacheID) {
		thumb, err := task.Wait(ctx)
		if err == nil {
			return thumb, nil
		}
	}

	os.MkdirAll(t.CacheDir, os.ModePerm)

	err := t.GenerateFunc(ctx, f, src, cachePath)
	t.finish(cacheID, thumb, err)
	return thumb, err
}

func (t *CachedThumbnailer) prepare(id string) (*promise[*Thumbnail], bool) {
	t.locker.Lock()
	defer t.locker.Unlock()
	if task, ok := t.generating[id]; ok {
		return task, true
	}
	t.generating[id] = NewPromise[*Thumbnail]()
	return t.generating[id], false
}

func (t *CachedThumbnailer) finish(id string, thumb *Thumbnail, err error) {
	t.locker.Lock()
	defer t.locker.Unlock()
	if task, ok := t.generating[id]; ok {
		delete(t.generating, id)
		task.Resolve(thumb, err)
	}
}

func NewImageThumbnailer(cacheDir string) *CachedThumbnailer {
	return &CachedThumbnailer{
		CacheDir:      cacheDir,
		SupportedFunc: isSupportedImage,
		GenerateFunc:  makeImageThumbnail,
		generating:    map[string]*promise[*Thumbnail]{},
	}
}

func isSupportedImage(typ string) bool {
	return typ == "image/jpeg" || typ == "image/png" || typ == "image/gif" || typ == "image/bmp"
}

func makeImageThumbnail(ctx context.Context, f fs.FS, src, out string) error {
	in, err := f.Open(src)
	if err != nil {
		return err
	}
	img, _, err := image.Decode(in)
	if err != nil {
		return err
	}
	timg := resize.Resize(ThumbnailWidth, 0, img, resize.Lanczos3)

	thumb, err := os.Create(out)
	if err != nil {
		return err
	}
	defer thumb.Close()
	return jpeg.Encode(thumb, timg, nil)
}

func NewVideoThumbnailer(cacheDir, ffmpegPath string) *CachedThumbnailer {
	return &CachedThumbnailer{
		CacheDir:      cacheDir,
		SupportedFunc: isSupportedVideo,
		GenerateFunc: func(ctx context.Context, f fs.FS, src, dst string) error {
			return makeVideoThumbnail(ctx, f, src, dst, ffmpegPath)
		},
	}
}

type RealPathResolver interface {
	RealPath(string) string
}

func isSupportedVideo(typ string) bool {
	return strings.HasPrefix(typ, "video/")
}

func makeVideoThumbnail(ctx context.Context, f fs.FS, in, out string, ffmpegPath string) error {
	if rpr, ok := f.(RealPathResolver); ok {
		in = rpr.RealPath(in)
	}
	scaleOpt := fmt.Sprintf("scale=%d:-1", ThumbnailWidth)
	args := []string{"-ss", "3", "-i", in, "-vframes", "1", "-vcodec", "mjpeg", "-an", "-vf", scaleOpt, out}
	if strings.HasPrefix(in, "https://") || strings.HasPrefix(in, "http://") {
		// To prevent hostname resolving issue
		if parsedURL, err := url.Parse(in); err == nil {
			log.Println("Resolve hostname...", parsedURL.Host)
			if addrs, err := net.LookupHost(parsedURL.Host); err == nil {
				hostHeader := "Host: " + parsedURL.Host
				parsedURL.Host = addrs[0]
				args[3] = parsedURL.String()
				args = append([]string{"-headers", hostHeader}, args...)
			}
		}
	}
	c := exec.CommandContext(ctx, ffmpegPath, args...)
	err := c.Start()
	if err != nil {
		log.Println(ffmpegPath, args)
		return nil
	}
	err = c.Wait()
	_, err2 := os.Stat(out)
	if err == nil && err2 != nil {
		log.Println("RETRY ", ffmpegPath, "-i", in, "-vframes", "1",
			"-vcodec", "mjpeg", "-an", "-vf", scaleOpt, out)
		// TODO
		c := exec.CommandContext(ctx, ffmpegPath, "-i", in, "-vframes", "1",
			"-vcodec", "mjpeg", "-an", "-vf", scaleOpt, out)
		err = c.Start()
		err = c.Wait()
	}
	return err
}
