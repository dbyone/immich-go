// Package media provides image probing and thumbnail generation. The
// upstream server uses sharp; this port uses the Go standard library plus
// golang.org/x/image for high-quality resampling.
package media

import (
	"bytes"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/image/draw"
)

// ThumbnailMax and PreviewMax match the Immich rendition sizes
// (250px thumbnails, 1440px previews).
const (
	ThumbnailMax = 250
	PreviewMax   = 1440
	JPEGQuality  = 80
)

// Probe reads image dimensions without fully decoding the image.
func Probe(path string) (width, height int, format string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, "", err
	}
	defer f.Close()
	cfg, format, err := image.DecodeConfig(f)
	if err != nil {
		return 0, 0, "", err
	}
	return cfg.Width, cfg.Height, format, nil
}

// GenerateThumb renders a resized JPEG whose longest edge is maxEdge.
// When the source cannot be decoded the original bytes are returned
// unchanged so clients still receive a usable response.
func GenerateThumb(path string, maxEdge int) ([]byte, error) {
	src, err := loadImage(path)
	if err != nil {
		return os.ReadFile(path)
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return nil, fmt.Errorf("invalid image %s", path)
	}

	scale := float64(maxEdge) / float64(max(w, h))
	if scale > 1 {
		scale = 1
	}
	dw, dh := max(1, int(float64(w)*scale+0.5)), max(1, int(float64(h)*scale+0.5))
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	draw.CatmullRom.Scale(dst, dst.Rect, src, b, draw.Over, nil)

	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: JPEGQuality}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func loadImage(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return png.Decode(f)
	case ".gif":
		return gif.Decode(f)
	default:
		return jpeg.Decode(f)
	}
}
