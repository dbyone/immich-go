package videometa

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"immich-go/internal/videometa/videotest"
)

func mustParseMP4(t *testing.T, b []byte) *Info {
	t.Helper()
	info, err := ParseMP4(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatalf("parse mp4: %v", err)
	}
	return info
}

func TestMP4BasicVideo(t *testing.T) {
	info := mustParseMP4(t, videotest.BuildMP4(videotest.Options{
		Width: 1920, Height: 1080, DurationMs: 12_500, FPS: 25,
	}))
	if info.Width != 1920 || info.Height != 1080 {
		t.Fatalf("dims: %dx%d", info.Width, info.Height)
	}
	if info.DurationMs != 12_500 {
		t.Fatalf("duration: %d", info.DurationMs)
	}
	if math.Abs(info.FPS-25) > 0.01 {
		t.Fatalf("fps: %f", info.FPS)
	}
	if info.VideoCodec != "h264" {
		t.Fatalf("video codec: %s", info.VideoCodec)
	}
	if info.AudioCodec != "aac" {
		t.Fatalf("audio codec: %s", info.AudioCodec)
	}
	if info.RotationDeg != 0 {
		t.Fatalf("rotation: %d", info.RotationDeg)
	}
}

func TestMP4RotationSwapsDimensions(t *testing.T) {
	for _, tc := range []struct{ rot, wantW, wantH int }{
		{90, 1080, 1920}, {270, 1080, 1920}, {180, 1920, 1080}, {0, 1920, 1080},
	} {
		info := mustParseMP4(t, videotest.BuildMP4(videotest.Options{
			Width: 1920, Height: 1080, DurationMs: 1000, FPS: 25, RotationDeg: tc.rot,
		}))
		if info.RotationDeg != tc.rot {
			t.Fatalf("rot %d: parsed rotation %d", tc.rot, info.RotationDeg)
		}
		if info.Width != tc.wantW || info.Height != tc.wantH {
			t.Fatalf("rot %d: dims %dx%d, want %dx%d", tc.rot, info.Width, info.Height, tc.wantW, tc.wantH)
		}
	}
}

func TestMP4NonIntegerFPSApproximates(t *testing.T) {
	info := mustParseMP4(t, videotest.BuildMP4(videotest.Options{
		Width: 3840, Height: 2160, DurationMs: 10_000, FPS: 29.97,
	}))
	if math.Abs(info.FPS-30.0) > 0.5 {
		t.Fatalf("29.97fps should approximate 30, got %f", info.FPS)
	}
	if info.DurationMs != 10_000 {
		t.Fatalf("duration: %d", info.DurationMs)
	}
}

func TestMP4CodecsAndNoAudio(t *testing.T) {
	info := mustParseMP4(t, videotest.BuildMP4(videotest.Options{
		Width: 1280, Height: 720, DurationMs: 4000, FPS: 30,
		VideoFourCC: "hvc1", AudioFourCC: "none",
	}))
	if info.VideoCodec != "hevc" {
		t.Fatalf("codec: %s", info.VideoCodec)
	}
	if info.AudioCodec != "" {
		t.Fatalf("audio should be absent, got %s", info.AudioCodec)
	}
}

func TestMP4RejectsGarbage(t *testing.T) {
	cases := [][]byte{
		{},
		bytes.Repeat([]byte{0xFF}, 64),
		[]byte("ftypisom"),               // not a real ftyp box
		{0, 0, 0, 20, 'f', 't', 'y', 'p'}, // truncated ftyp
	}
	for i, c := range cases {
		if _, err := ParseMP4(bytes.NewReader(c), int64(len(c))); err == nil {
			t.Fatalf("case %d: expected error", i)
		}
	}
}

func TestMP4MoovAfterMissingMdat(t *testing.T) {
	// moov may appear anywhere; findMoov skips preceding boxes.
	data := videotest.BuildMP4(videotest.Options{Width: 640, Height: 480, DurationMs: 2000, FPS: 24})
	ftypLen := int(binary.BigEndian.Uint32(data[0:4]))
	buf := append([]byte{}, data[:ftypLen]...)       // full ftyp box
	buf = append(buf, boxForTest("mdat", []byte("junkjunk"))...)
	buf = append(buf, data[ftypLen:]...)             // moov
	info := mustParseMP4(t, buf)
	if info.Width != 640 || info.DurationMs != 2000 {
		t.Fatalf("moov after mdat: %+v", info)
	}
}

func boxForTest(typ string, payload []byte) []byte {
	box := make([]byte, 8+len(payload))
	box[0], box[1], box[2] = 0, 0, byte((8 + len(payload)) >> 8)
	box[3] = byte(8 + len(payload))
	copy(box[4:8], typ)
	copy(box[8:], payload)
	return box
}
