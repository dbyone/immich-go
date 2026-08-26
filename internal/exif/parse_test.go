package exif

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"testing"
	"time"

	"immich-go/internal/exif/exiftest"
)

func mustParse(t *testing.T, b []byte) *Data {
	t.Helper()
	d, err := Parse(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return d
}

func TestRoundTripLittleEndian(t *testing.T) {
	taken := time.Date(2026, 3, 14, 9, 26, 53, 0, time.UTC)
	lat, lon := 31.2304, 121.4737 // Shanghai
	jpg := exiftest.BuildJPEG(exiftest.Options{
		Width: 4032, Height: 3024,
		Make: "Canon", Model: "EOS R5", LensModel: "RF 50mm F1.2",
		Description:    "Shanghai skyline at dusk",
		Orientation:    6, // rotate 90 CW
		Rating:         4,
		DateTimeOriginal: &taken,
		Latitude:       &lat,
		Longitude:      &lon,
	})

	d := mustParse(t, jpg)
	if d.Make != "Canon" || d.Model != "EOS R5" || d.LensModel != "RF 50mm F1.2" {
		t.Fatalf("camera tags: %+v", d)
	}
	if d.Description != "Shanghai skyline at dusk" {
		t.Fatalf("description: %q", d.Description)
	}
	if d.Orientation != 6 {
		t.Fatalf("orientation: %d", d.Orientation)
	}
	if d.Rating == nil || *d.Rating != 4 {
		t.Fatalf("rating: %v", d.Rating)
	}
	if d.Width != 4032 || d.Height != 3024 {
		t.Fatalf("pixel dims: %dx%d", d.Width, d.Height)
	}
	if d.DateTimeOriginal == nil || !d.DateTimeOriginal.Equal(taken) {
		t.Fatalf("dateTimeOriginal: %v", d.DateTimeOriginal)
	}
	if d.Latitude == nil || math.Abs(*d.Latitude-lat) > 1e-4 {
		t.Fatalf("latitude: %v", d.Latitude)
	}
	if d.Longitude == nil || math.Abs(*d.Longitude-lon) > 1e-4 {
		t.Fatalf("longitude: %v", d.Longitude)
	}

	// The spliced file must still decode as a real JPEG.
	img, err := jpeg.Decode(bytes.NewReader(jpg))
	if err != nil {
		t.Fatalf("jpeg decode: %v", err)
	}
	if img.Bounds().Dx() != 4032 || img.Bounds().Dy() != 3024 {
		t.Fatalf("decoded bounds: %v", img.Bounds())
	}
}

func TestRoundTripBigEndianAndNegativeCoords(t *testing.T) {
	taken := time.Date(2025, 12, 1, 23, 59, 59, 0, time.UTC)
	lat, lon := -33.8688, -151.2093 // Sydney
	jpg := exiftest.BuildJPEG(exiftest.Options{
		Width: 100, Height: 80, BigEndian: true,
		Make: "NIKON CORPORATION", Model: "NIKON Z 8",
		DateTimeOriginal: &taken,
		Latitude:         &lat,
		Longitude:        &lon,
	})
	d := mustParse(t, jpg)
	if d.Make != "NIKON CORPORATION" || d.Model != "NIKON Z 8" {
		t.Fatalf("camera tags (MM): %+v", d)
	}
	if d.DateTimeOriginal == nil || !d.DateTimeOriginal.Equal(taken) {
		t.Fatalf("date (MM): %v", d.DateTimeOriginal)
	}
	if d.Latitude == nil || *d.Latitude < -33.869 || *d.Latitude > -33.868 {
		t.Fatalf("latitude (MM): %v", d.Latitude)
	}
	if d.Longitude == nil || *d.Longitude < -151.210 || *d.Longitude > -151.208 {
		t.Fatalf("longitude (MM): %v", d.Longitude)
	}
}

func TestNoEXIF(t *testing.T) {
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for i := 0; i < 64; i++ {
		img.Set(i%8, i/8, color.RGBA{R: 255, A: 255})
	}
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	d := mustParse(t, buf.Bytes())
	if d.Make != "" || d.DateTimeOriginal != nil || d.Latitude != nil {
		t.Fatalf("expected empty data, got %+v", d)
	}
}

func TestGarbageInput(t *testing.T) {
	cases := [][]byte{
		{},
		{0xFF, 0xD8},
		{0xFF, 0xD8, 0xFF, 0xE1, 0x00, 0x02},            // truncated APP1
		{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x04, 'J', 'F'},  // JFIF only
		[]byte("Exif\x00\x00MM"),                        // truncated TIFF
		{0xFF, 0xD8, 0xFF, 0xE1, 0x00, 0x08, 'E', 'x', 'i', 'f', 0, 0, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
	}
	for i, c := range cases {
		d, err := Parse(bytes.NewReader(c), int64(len(c)))
		if err != nil {
			t.Fatalf("case %d must not error: %v", i, err)
		}
		if d == nil {
			t.Fatalf("case %d returned nil data", i)
		}
	}
}

func TestHostileOffsetsDoNotPanic(t *testing.T) {
	// Valid header, absurd IFD offset and count values.
	hostile := append([]byte("II\x2a\x00\xff\xff\xff\xff"),
		make([]byte, 64)...)
	// Fill with 0xFF to maximize bogus offsets/counts.
	for i := range hostile[8:] {
		hostile[8+i] = 0xFF
	}
	d, err := Parse(bytes.NewReader(hostile), int64(len(hostile)))
	if err != nil {
		t.Fatalf("hostile input must not error: %v", err)
	}
	_ = d
}

func TestExifTimeFormats(t *testing.T) {
	ts := parseExifTime("2026:08:24 10:11:12", "")
	if ts == nil || ts.Year() != 2026 || ts.Month() != time.August || ts.Hour() != 10 {
		t.Fatalf("standard format: %v", ts)
	}
	ts = parseExifTime("2026-08-24 10:11:12", "")
	if ts == nil || ts.Day() != 24 {
		t.Fatalf("dash format: %v", ts)
	}
	ts = parseExifTime("2026:08:24 10:11:12", "5")
	if ts == nil || ts.Nanosecond() != 500_000_000 {
		t.Fatalf("subsecond: %v", ts)
	}
	if parseExifTime("garbage", "") != nil {
		t.Fatal("garbage must be nil")
	}
	if parseExifTime("", "") != nil {
		t.Fatal("empty must be nil")
	}
}

func TestUserComment(t *testing.T) {
	b := append([]byte("ASCII\x00\x00\x00"), []byte("hello world")...)
	if got := userComment(b); got != "hello world" {
		t.Fatalf("prefixed comment: %q", got)
	}
	if got := userComment([]byte("raw")); got != "raw" {
		t.Fatalf("raw comment: %q", got)
	}
}
