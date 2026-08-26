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
	return resizeToJPEG(src, maxEdge)
}

// GenerateThumbFromBytes resizes an in-memory JPEG (e.g. an extracted
// video frame) to the given bounding box.
func GenerateThumbFromBytes(b []byte, maxEdge int) ([]byte, error) {
	src, err := jpeg.Decode(bytes.NewReader(b))
	if err != nil {
		return b, nil // pass undecodable frames through untouched
	}
	return resizeToJPEG(src, maxEdge)
}

func resizeToJPEG(src image.Image, maxEdge int) ([]byte, error) {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return nil, fmt.Errorf("invalid image")
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

// CropFace crops the face bounding box (x1,y1,x2,y2) from an image on
// disk with a small margin and renders a maxEdge JPEG — the person avatar.
func CropFace(path string, box [4]int, maxEdge int) ([]byte, error) {
	src, err := loadImage(path)
	if err != nil {
		return os.ReadFile(path)
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	// 25% margin around the detected face, floored at 8px.
	m := (box[2] - box[0]) / 4
	if m < 8 {
		m = 8
	}
	x1 := clampInt(box[0]-m, 0, w-1)
	y1 := clampInt(box[1]-m, 0, h-1)
	x2 := clampInt(box[2]+m, x1+1, w)
	y2 := clampInt(box[3]+m, y1+1, h)

	type subImager interface {
		SubImage(r image.Rectangle) image.Image
	}
	croppable, ok := src.(subImager)
	if !ok {
		return resizeToJPEG(src, maxEdge)
	}
	crop := croppable.SubImage(image.Rect(b.Min.X+x1, b.Min.Y+y1, b.Min.X+x2, b.Min.Y+y2))
	return resizeToJPEG(crop, maxEdge)
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
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
