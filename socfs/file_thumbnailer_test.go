package socfs

import (
	"context"
	"os"
	"testing"
)

func TestGetThumbnail(t *testing.T) {
	thumbnailer := NewImageThumbnailer("cache")
	thumb, err := thumbnailer.GetThumbnail(context.Background(), os.DirFS("testdata"), "test.png", "image/png", nil)
	if err != nil {
		t.Fatal(err)
	}
	if thumb == nil {
		t.Fatal("thumb should not be null")
	}
}
