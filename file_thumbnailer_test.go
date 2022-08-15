package main

import (
	"context"
	"os"
	"testing"
)

func TestGetThumbnail(t *testing.T) {
	thumbnailer := NewImageThumbnailer("cache")
	thumb := thumbnailer.GetThumbnail(context.Background(), os.DirFS("testdata"), "test.png", "image/png", nil)
	if thumb == nil {
		t.Fatal("thumb should not be null")
	}
}
