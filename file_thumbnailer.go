package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
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
	GetThumbnail(ctx context.Context, f fs.FS, path, typ string, opt any) *Thumbnail
}

var DefaultThumbnailer = ThumbnailerGroup{}

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

func (g ThumbnailerGroup) GetThumbnail(ctx context.Context, f fs.FS, path, typ string, opt any) *Thumbnail {
	for _, t := range g.Thumbnailers {
		if t.Supported(typ) {
			return t.GetThumbnail(ctx, f, path, typ, opt)
		}
	}
	return nil
}

func (g *ThumbnailerGroup) Register(t Thumbnailer) {
	g.Thumbnailers = append(g.Thumbnailers, t)
}

type CachedThumbnailer struct {
	CacheDir      string
	SupportedFunc func(typ string) bool
	GenerateFunc  func(ctx context.Context, f fs.FS, src, dst string) error
	locker        sync.Mutex
	generating    map[string]bool
}

func (t *CachedThumbnailer) Supported(typ string) bool {
	return t.SupportedFunc(typ)
}

func (t *CachedThumbnailer) GetThumbnail(ctx context.Context, f fs.FS, src, typ string, opt any) *Thumbnail {
	sum := sha1.Sum([]byte(src))
	cacheID := hex.EncodeToString(sum[:])
	cachePath := path.Join(t.CacheDir, cacheID+".jpeg")
	thumb := &Thumbnail{Path: cachePath, Type: "image/jpeg"}

	if _, err := os.Stat(cachePath); err == nil {
		return thumb
	}

	if !t.prepare(cacheID) {
		return nil // TODO
	}

	os.MkdirAll(t.CacheDir, os.ModePerm)

	err := t.GenerateFunc(ctx, f, src, cachePath)
	if err != nil {
		return nil
	}
	t.finish(cacheID, thumb)

	return thumb
}

func (t *CachedThumbnailer) prepare(id string) bool {
	t.locker.Lock()
	defer t.locker.Unlock()
	if t.generating[id] {
		return false
	}
	t.generating[id] = true
	return true
}

func (t *CachedThumbnailer) finish(id string, thumb *Thumbnail) {
	t.locker.Lock()
	defer t.locker.Unlock()
	delete(t.generating, id)
	// TODO
}

func NewImageThumbnailer(cacheDir string) *CachedThumbnailer {
	return &CachedThumbnailer{
		CacheDir:      cacheDir,
		SupportedFunc: isSupportedImage,
		GenerateFunc:  makeImageThumbnail,
		generating:    map[string]bool{},
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
	timg := resize.Resize(160, 0, img, resize.Lanczos3)

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
	args := []string{"-ss", "3", "-i", in, "-vframes", "1", "-vcodec", "mjpeg", "-an", "-vf", "scale=200:-1", out}
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
			"-vcodec", "mjpeg", "-an", "-vf", "scale=200:-1", out)
		// TODO
		c := exec.CommandContext(ctx, ffmpegPath, "-i", in, "-vframes", "1",
			"-vcodec", "mjpeg", "-an", "-vf", "scale=200:-1", out)
		err = c.Start()
		err = c.Wait()
	}
	return err
}
