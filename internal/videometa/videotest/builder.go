// Package videotest builds minimal MP4 containers with crafted metadata.
// Like exiftest it exists purely as test support — the videometa parser
// tests and the API e2e suite need deterministic video fixtures without
// shipping real media samples.
package videotest

import (
	"encoding/binary"
	"math"
)

// Options describes the MP4 to synthesize. Zero values get defaults.
type Options struct {
	Width, Height int
	DurationMs    int64
	FPS           float64
	VideoFourCC   string // default avc1
	AudioFourCC   string // default mp4a; "none" omits the audio track
	RotationDeg   int    // 0/90/180/270 via the tkhd display matrix
}

// BuildMP4 renders an ftyp+moov file. There is no mdat — the parser (and
// ffprobe) only need the moov tree for metadata.
func BuildMP4(o Options) []byte {
	if o.Width <= 0 {
		o.Width = 1920
	}
	if o.Height <= 0 {
		o.Height = 1080
	}
	if o.DurationMs <= 0 {
		o.DurationMs = 10_000
	}
	if o.FPS <= 0 {
		o.FPS = 25
	}
	if o.VideoFourCC == "" {
		o.VideoFourCC = "avc1"
	}

	const timescale = 1000

	mvhd := box("mvhd", concat(
		u32(0),         // version 0 / flags
		u32(0), u32(0), // creation / modification
		u32(timescale), // movie timescale
		u32(uint32(o.DurationMs)),
		make([]byte, 64), // rate/volume/matrix/next-track filler
	))

	videoTrak := box("trak", concat(
		tkhdBox(o.Width, o.Height, o.RotationDeg),
		box("mdia", concat(
			hdlrBox("vide"),
			mdhdBox(o.DurationMs),
			box("minf", box("stbl", concat(
				videoSTSD(o.VideoFourCC, o.Width, o.Height),
				sttsBox(o.DurationMs, o.FPS),
			))),
		)),
	))

	moovChildren := concat(mvhd, videoTrak)
	if o.AudioFourCC != "none" {
		if o.AudioFourCC == "" {
			o.AudioFourCC = "mp4a"
		}
		audioTrak := box("trak", concat(
			tkhdBox(0, 0, 0),
			box("mdia", concat(
				hdlrBox("soun"),
				mdhdBox(o.DurationMs),
				box("minf", box("stbl", concat(
					audioSTSD(o.AudioFourCC),
					sttsBox(o.DurationMs, 1),
				))),
			)),
		))
		moovChildren = append(moovChildren, audioTrak...)
	}
	moov := box("moov", moovChildren)

	ftyp := box("ftyp", concat([]byte("isom"), u32(0x200), []byte("isomiso2mp41")))
	return concat(ftyp, moov)
}

// ---- atom helpers ----

func box(typ string, payload []byte) []byte {
	out := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint32(out[0:4], uint32(8+len(payload)))
	copy(out[4:8], typ)
	copy(out[8:], payload)
	return out
}

func concat(parts ...[]byte) []byte {
	var total int
	for _, p := range parts {
		total += len(p)
	}
	out := make([]byte, 0, total)
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func u32(v uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return b
}

func u16(v uint16) []byte {
	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, v)
	return b
}

func fixed16(v int) uint32 { return uint32(v) << 16 }

// tkhdBox lays out the v0 track header with the display matrix before
// width/height (both fixed-point 16.16).
func tkhdBox(width, height, rotation int) []byte {
	payload := concat(
		u32(0x00000003), // version 0, flags: track_enabled|in_movie
		u32(0), u32(0),  // creation / modification
		u32(1), u32(0), // track id / reserved
		u32(10_000),     // duration (movie units; unused by the parser)
		make([]byte, 8), // reserved
		u16(0), u16(0),  // layer / alternate group
		u16(0), u16(0), // volume / reserved
	)
	payload = append(payload, rotationMatrix(rotation)...)
	payload = append(payload, u32(fixed16(width))...)
	payload = append(payload, u32(fixed16(height))...)
	return box("tkhd", payload)
}

func rotationMatrix(deg int) []byte {
	one := uint32(1) << 16
	var m [9]uint32
	for i := range m {
		m[i] = 0
	}
	m[8] = one
	switch deg {
	case 90:
		m[1], m[3] = one, ^one+1 // 0x00010000, 0xFFFF0000
	case 180:
		m[0], m[4] = ^one+1, ^one+1
	case 270:
		m[1], m[3] = ^one+1, one
	default:
		m[0], m[4] = one, one
	}
	out := make([]byte, 36)
	for i, v := range m {
		binary.BigEndian.PutUint32(out[i*4:i*4+4], v)
	}
	return out
}

func hdlrBox(handler string) []byte {
	return box("hdlr", concat(
		u32(0),           // version / flags
		u32(0),           // pre_defined
		[]byte(handler),  // handler_type
		make([]byte, 12), // reserved
		[]byte{0},        // name (empty, NUL-terminated)
	))
}

func mdhdBox(durationMs int64) []byte {
	return box("mdhd", concat(
		u32(0),         // version 0 / flags
		u32(0), u32(0), // creation / modification
		u32(1000),               // media timescale
		u32(uint32(durationMs)), // duration in timescale units
		u16(0x55C4), u16(0),     // language (und) / pre_defined
	))
}

// videoSTSD emits one VisualSampleEntry: coded dimensions at entry
// offsets 32/34 per the ISO spec.
func videoSTSD(fourcc string, width, height int) []byte {
	entry := concat(
		u32(36),         // entry size (self-inclusive)
		[]byte(fourcc),  // format
		make([]byte, 6), // reserved
		u16(1),          // data_reference_index
		u16(0), u16(0),  // pre_defined / reserved
		make([]byte, 12), // pre_defined[3]
		u16(uint16(width)),
		u16(uint16(height)),
	)
	if len(entry) != 36 {
		panic("video entry layout drifted")
	}
	return box("stsd", concat(u32(0), u32(1), entry))
}

func audioSTSD(fourcc string) []byte {
	entry := concat(
		u32(36),
		[]byte(fourcc),
		make([]byte, 6),
		u16(1),
		make([]byte, 8), // reserved[2]
		u16(2), u16(16), // channels / sample size
		u32(0),         // pre_defined
		u32(44100<<16), // sample rate (16.16)
	)
	return box("stsd", concat(u32(0), u32(1), entry))
}

// sttsBox emits a single (count, delta) run reproducing the target fps
// in the 1000-unit media timescale.
func sttsBox(durationMs int64, fps float64) []byte {
	delta := int64(math.Round(1000 / fps))
	if delta < 1 {
		delta = 1
	}
	count := durationMs / delta
	if count < 1 {
		count = 1
	}
	return box("stts", concat(u32(0), u32(1), u32(uint32(count)), u32(uint32(delta))))
}
