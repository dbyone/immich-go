// Package videometa extracts video metadata without external tools.
//
// MP4/MOV/M4V containers (the overwhelming majority of phone videos) are
// parsed natively from their atom structure — duration, pixel dimensions,
// rotation, frame rate and codec ids, all without shelling out. Other
// containers fall back to ffprobe when the ffmpeg suite is installed.
package videometa

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// Info is the metadata subset the server persists for videos.
type Info struct {
	DurationMs  int64
	Width       int
	Height      int
	RotationDeg int // 0/90/180/270 from the track display matrix
	FPS         float64
	VideoCodec  string // h264, hevc, av1, vp9, ...
	AudioCodec  string // aac, opus, ...
}

// ParseFile probes a video file: the pure-Go MP4 parser runs first and
// ffprobe backs it up for exotic containers.
func ParseFile(path string) (*Info, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info, err := ParseMP4(f, fi.Size()); err == nil && info != nil {
		return info, nil
	}
	return probeFFprobe(path)
}

// ---- MP4 atom walking ----

var containerAtoms = map[string]bool{
	"moov": true, "trak": true, "mdia": true, "minf": true, "stbl": true,
	"udta": true, "edts": true, "dinf": true,
}

// ParseMP4 reads the moov tree of an ISO-BMFF file.
func ParseMP4(r io.ReaderAt, size int64) (*Info, error) {
	head := make([]byte, 12)
	if _, err := r.ReadAt(head, 0); err != nil {
		return nil, err
	}
	if string(head[4:8]) != "ftyp" {
		return nil, fmt.Errorf("not an MP4 family file")
	}
	// Read the whole box tree except mdat (which can be huge); the moov
	// atom is small but may live after mdat, so index top-level boxes
	// first and only load moov.
	moov, err := findMoov(r, size)
	if err != nil {
		return nil, err
	}
	info := &Info{}
	if err := walkAtoms(moov, "moov", info); err != nil {
		return nil, err
	}
	if info.DurationMs == 0 && info.Width == 0 {
		return nil, fmt.Errorf("no usable tracks")
	}
	if info.RotationDeg == 90 || info.RotationDeg == 270 {
		info.Width, info.Height = info.Height, info.Width
	}
	return info, nil
}

func readBoxHeader(r io.ReaderAt, off int64) (typ string, hdrLen int, bodyLen int64, next int64, err error) {
	var h [16]byte
	if _, err := r.ReadAt(h[:8], off); err != nil {
		return "", 0, 0, 0, err
	}
	size := int64(binary.BigEndian.Uint32(h[0:4]))
	typ = string(h[4:8])
	hdrLen = 8
	if size == 1 { // 64-bit size follows the type
		if _, err := r.ReadAt(h[8:16], off+8); err != nil {
			return "", 0, 0, 0, err
		}
		size = int64(binary.BigEndian.Uint64(h[8:16]))
		hdrLen = 16
	}
	if size < int64(hdrLen) {
		return "", 0, 0, 0, fmt.Errorf("invalid box size %d for %s", size, typ)
	}
	return typ, hdrLen, size - int64(hdrLen), off + size, nil
}

func findMoov(r io.ReaderAt, size int64) ([]byte, error) {
	off := int64(0)
	for off < size {
		typ, hdrLen, bodyLen, next, err := readBoxHeader(r, off)
		if err != nil {
			return nil, err
		}
		if typ == "moov" {
			if bodyLen > 256<<20 {
				return nil, fmt.Errorf("moov box unreasonably large")
			}
			body := make([]byte, bodyLen)
			if _, err := r.ReadAt(body, off+int64(hdrLen)); err != nil {
				return nil, err
			}
			return body, nil
		}
		if next <= off {
			return nil, fmt.Errorf("non-advancing box")
		}
		off = next
	}
	return nil, fmt.Errorf("moov box not found")
}

// walkAtoms recursively descends container boxes, harvesting tags.
func walkAtoms(body []byte, parent string, info *Info) error {
	off := 0
	for off+8 <= len(body) {
		size := int(binary.BigEndian.Uint32(body[off : off+4]))
		typ := string(body[off+4 : off+8])
		hdrLen := 8
		if size == 1 && off+16 <= len(body) {
			size = int(binary.BigEndian.Uint64(body[off+8 : off+16]))
			hdrLen = 16
		}
		if size < hdrLen || off+size > len(body) {
			break // truncated or malformed; keep what we have
		}
		content := body[off+hdrLen : off+size]

		switch typ {
		case "mvhd":
			parseMVHD(content, info)
		case "trak":
			// Each trak gets a fresh accumulator; walkTrak fills it and
			// mergeTrack folds the result into Info.
			ts := &trackState{}
			walkTrak(content, ts)
			mergeTrack(ts, info)
		default:
			if containerAtoms[typ] && typ != "trak" {
				if err := walkAtoms(content, typ, info); err != nil {
					return err
				}
			}
		}
		off += size
	}
	return nil
}

// trackState accumulates one trak subtree.
type trackState struct {
	handler    string // vide / soun
	trackDurMs int64
	timescale  int64 // mdhd media timescale (feeds the fps math)
	width      int
	height     int
	rotation   int
	codec      string
	fps        float64
}

func walkTrak(trak []byte, ts *trackState) {
	off := 0
	for off+8 <= len(trak) {
		size := int(binary.BigEndian.Uint32(trak[off : off+4]))
		typ := string(trak[off+4 : off+8])
		if size < 8 || off+size > len(trak) {
			break
		}
		content := trak[off+8 : off+size]
		switch typ {
		case "tkhd":
			parseTKHD(content, ts)
		case "mdia":
			walkMdia(content, ts)
		}
		off += size
	}
}

func walkMdia(mdia []byte, ts *trackState) {
	off := 0
	for off+8 <= len(mdia) {
		size := int(binary.BigEndian.Uint32(mdia[off : off+4]))
		typ := string(mdia[off+4 : off+8])
		if size < 8 || off+size > len(mdia) {
			break
		}
		content := mdia[off+8 : off+size]
		switch typ {
		case "hdlr":
			if len(content) >= 12 {
				ts.handler = string(content[8:12])
			}
		case "mdhd":
			ts.timescale, ts.trackDurMs = parseMDHD(content)
		case "minf":
			walkMinf(content, ts)
		}
		off += size
	}
}

func walkMinf(minf []byte, ts *trackState) {
	off := 0
	for off+8 <= len(minf) {
		size := int(binary.BigEndian.Uint32(minf[off : off+4]))
		typ := string(minf[off+4 : off+8])
		if size < 8 || off+size > len(minf) {
			break
		}
		content := minf[off+8 : off+size]
		if typ == "stbl" {
			walkStbl(content, ts)
		}
		off += size
	}
}

func walkStbl(stbl []byte, ts *trackState) {
	off := 0
	for off+8 <= len(stbl) {
		size := int(binary.BigEndian.Uint32(stbl[off : off+4]))
		typ := string(stbl[off+4 : off+8])
		if size < 8 || off+size > len(stbl) {
			break
		}
		content := stbl[off+8 : off+size]
		switch typ {
		case "stsd":
			codec, w, h := parseSTSD(content)
			ts.codec = codec
			if w > 0 && h > 0 {
				ts.width, ts.height = w, h
			}
		case "stts":
			ts.fps = parseSTTSFPS(content, ts)
		}
		off += size
	}
}

// parseMVHD reads the movie header for the global duration.
func parseMVHD(b []byte, info *Info) {
	if len(b) < 4 {
		return
	}
	version := b[0]
	switch version {
	case 0:
		if len(b) < 20 {
			return
		}
		timescale := binary.BigEndian.Uint32(b[12:16])
		duration := binary.BigEndian.Uint32(b[16:20])
		info.DurationMs = durationToMs(int64(duration), int64(timescale))
	case 1:
		if len(b) < 32 {
			return
		}
		timescale := binary.BigEndian.Uint32(b[20:24])
		duration := int64(binary.BigEndian.Uint64(b[24:32]))
		info.DurationMs = durationToMs(duration, int64(timescale))
	}
}

func durationToMs(d, timescale int64) int64 {
	if timescale <= 0 || d <= 0 {
		return 0
	}
	return d * 1000 / timescale
}

// parseTKHD extracts display dimensions and rotation.
func parseTKHD(b []byte, ts *trackState) {
	if len(b) < 4 {
		return
	}
	version := b[0]
	var widthOff int
	switch version {
	case 0:
		widthOff = 76
	case 1:
		widthOff = 88
	default:
		return
	}
	if len(b) >= widthOff+8 {
		w := fixed16(b[widthOff : widthOff+4])
		h := fixed16(b[widthOff+4 : widthOff+8])
		if w > 0 && h > 0 {
			ts.width, ts.height = w, h
		}
	}
	// Display matrix occupies the 36 bytes before width/height.
	mOff := widthOff - 36
	if mOff >= 0 && len(b) >= mOff+36 {
		ts.rotation = matrixRotation(b[mOff : mOff+36])
	}
}

func fixed16(b []byte) int {
	v := int(binary.BigEndian.Uint32(b))
	return v >> 16
}

// matrixRotation derives the quadrant rotation from the 2x2 part of the
// display matrix (values are 16.16 fixed point).
func matrixRotation(m []byte) int {
	read := func(i int) int {
		return int(int32(binary.BigEndian.Uint32(m[i*4 : i*4+4])))
	}
	a, b, c, d := read(0), read(1), read(3), read(4)
	switch {
	case a == 0 && b == 1<<16 && c == -(1<<16) && d == 0:
		return 90
	case a == -(1<<16) && b == 0 && c == 0 && d == -(1<<16):
		return 180
	case a == 0 && b == -(1<<16) && c == 1<<16 && d == 0:
		return 270
	default:
		return 0
	}
}

// parseMDHD reads the media header: timescale and duration (movie units).
func parseMDHD(b []byte) (timescale, durationMs int64) {
	if len(b) < 4 {
		return 0, 0
	}
	switch b[0] {
	case 0:
		if len(b) < 20 {
			return 0, 0
		}
		timescale = int64(binary.BigEndian.Uint32(b[12:16]))
		d := int64(binary.BigEndian.Uint32(b[16:20]))
		return timescale, durationToMs(d, timescale)
	case 1:
		if len(b) < 32 {
			return 0, 0
		}
		timescale = int64(binary.BigEndian.Uint32(b[20:24]))
		d := int64(binary.BigEndian.Uint64(b[24:32]))
		return timescale, durationToMs(d, timescale)
	}
	return 0, 0
}

// parseSTSD reads the first sample entry: codec fourcc and coded size.
func parseSTSD(b []byte) (codec string, width, height int) {
	if len(b) < 16 {
		return "", 0, 0
	}
	entrySize := int(binary.BigEndian.Uint32(b[8:12]))
	fourcc := string(b[12:16])
	// The entry starts at offset 8 but the box header (size+format) it
	// reports is inclusive, so the slice base is 12; the bound check must
	// match the slice, not the entry start.
	if entrySize < 36 || 12+entrySize > len(b) {
		return normalizeCodec(fourcc), 0, 0
	}
	entry := b[12 : 12+entrySize]
	// VisualSampleEntry: 8 (size+format) + 6 reserved + 2 dataRefIndex +
	// 2 preDefined + 2 reserved + 12 preDefined[3] puts width/height at
	// offsets 32/34 within the entry box.
	if isVideoCodec(fourcc) {
		width = int(binary.BigEndian.Uint16(entry[32:34]))
		height = int(binary.BigEndian.Uint16(entry[34:36]))
	}
	return normalizeCodec(fourcc), width, height
}

func isVideoCodec(fourcc string) bool {
	switch fourcc {
	case "avc1", "avc3", "hvc1", "hev1", "av01", "vp08", "vp09", "mp4v", "encv":
		return true
	}
	return false
}

func normalizeCodec(fourcc string) string {
	switch fourcc {
	case "avc1", "avc3", "encv":
		return "h264"
	case "hvc1", "hev1":
		return "hevc"
	case "av01":
		return "av1"
	case "vp08":
		return "vp8"
	case "vp09":
		return "vp9"
	case "mp4v":
		return "mpeg4"
	case "mp4a", "enca":
		return "aac"
	case "Opus", "opus":
		return "opus"
	case "ec-3":
		return "eac3"
	case "ac-3":
		return "ac3"
	default:
		return fourcc
	}
}

// parseSTTSFPS derives the average frame rate: total samples divided by
// their total duration in the track's mdhd timescale.
func parseSTTSFPS(b []byte, ts *trackState) float64 {
	if len(b) < 8 {
		return 0
	}
	timescale := ts.timescale
	if timescale <= 0 {
		timescale = 1000 // mdhd missing; assume movie units
	}
	entryCount := int(binary.BigEndian.Uint32(b[4:8]))
	off := 8
	var samples int64
	var totalUnits int64
	for i := 0; i < entryCount && off+8 <= len(b); i++ {
		count := int64(binary.BigEndian.Uint32(b[off : off+4]))
		delta := int64(binary.BigEndian.Uint32(b[off+4 : off+8]))
		samples += count
		totalUnits += count * delta
		off += 8
	}
	if samples <= 0 || totalUnits <= 0 || timescale <= 0 {
		return 0
	}
	seconds := float64(totalUnits) / float64(timescale)
	if seconds <= 0 {
		return 0
	}
	return float64(samples) / seconds
}

// mergeTrack folds a parsed trak into the file-level Info.
func mergeTrack(ts *trackState, info *Info) {
	switch ts.handler {
	case "vide":
		if ts.width > 0 {
			if info.Width == 0 {
				info.Width, info.Height = ts.width, ts.height
			}
			if info.RotationDeg == 0 {
				info.RotationDeg = ts.rotation
			}
		}
		if info.VideoCodec == "" {
			info.VideoCodec = ts.codec
		}
		if info.FPS == 0 && ts.fps > 0 {
			info.FPS = ts.fps
		}
		if info.DurationMs == 0 && ts.trackDurMs > 0 {
			info.DurationMs = ts.trackDurMs
		}
	case "soun":
		if info.AudioCodec == "" {
			info.AudioCodec = ts.codec
		}
	}
}
