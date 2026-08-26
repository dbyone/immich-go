// Package exiftest builds JPEG files carrying crafted EXIF metadata. It
// exists purely as test support — the parser's round-trip tests and the
// API end-to-end suite both need deterministic EXIF fixtures.
package exiftest

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"time"
)

// Options describes the EXIF payload to embed. Zero fields are omitted.
type Options struct {
	Width, Height          int
	Make, Model, LensModel string
	Description            string
	Orientation            uint16 // 1-8; 0 omits the tag
	Rating                 int    // 1-5; 0 omits the tag
	DateTimeOriginal       *time.Time
	Latitude, Longitude    *float64
	BigEndian              bool // emit an MM-order TIFF (exercises both parsers)
}

// BuildJPEG renders a real, decodable gradient JPEG and splices an EXIF
// APP1 segment right after the SOI marker — exactly where cameras put it.
func BuildJPEG(opts Options) []byte {
	w, h := opts.Width, opts.Height
	if w <= 0 || h <= 0 {
		w, h = 64, 64
	}
	base := renderJPEG(w, h)
	app1 := append([]byte("Exif\x00\x00"), buildTIFF(opts)...)
	seg := make([]byte, 4+len(app1))
	seg[0], seg[1] = 0xFF, 0xE1
	seg[2] = byte((len(app1) + 2) >> 8)
	seg[3] = byte(len(app1) + 2)
	copy(seg[4:], app1)

	out := make([]byte, 0, len(base)+len(seg))
	out = append(out, base[:2]...)
	out = append(out, seg...)
	out = append(out, base[2:]...)
	return out
}

func renderJPEG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{
				R: uint8(x * 255 / max(w-1, 1)),
				G: uint8(y * 255 / max(h-1, 1)),
				B: 128,
				A: 255,
			})
		}
	}
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90})
	return buf.Bytes()
}

// ---- TIFF assembly ----

type entry struct {
	tag, typ uint16
	count    uint32
	payload  []byte // full value bytes, encoded in the TIFF's byte order
	heapOff  uint32 // set when payload lives beyond the 4-byte value field
}

const (
	typASCII    = 2
	typSHORT    = 3
	typLONG     = 4
	typRATIONAL = 5
)

func buildTIFF(o Options) []byte {
	le := !o.BigEndian
	enc16 := func(v uint16) []byte {
		if le {
			return []byte{byte(v), byte(v >> 8)}
		}
		return []byte{byte(v >> 8), byte(v)}
	}
	enc32 := func(v uint32) []byte {
		if le {
			return []byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)}
		}
		return []byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}
	}
	asciiEntry := func(tag uint16, s string) entry {
		payload := append([]byte(s), 0)
		return entry{tag: tag, typ: typASCII, count: uint32(len(payload)), payload: payload}
	}
	shortEntry := func(tag, v uint16) entry {
		return entry{tag: tag, typ: typSHORT, count: 1, payload: enc16(v)}
	}
	longEntry := func(tag uint16, v uint32) entry {
		return entry{tag: tag, typ: typLONG, count: 1, payload: enc32(v)}
	}
	rational3Entry := func(tag uint16, rats [3][2]uint32) entry {
		payload := make([]byte, 0, 24)
		for _, r := range rats {
			payload = append(payload, enc32(r[0])...)
			payload = append(payload, enc32(r[1])...)
		}
		return entry{tag: tag, typ: typRATIONAL, count: 3, payload: payload}
	}
	// gpsRationals converts a signed coordinate into the hemisphere
	// reference plus degree/minute/second rationals; axis selects
	// N/S vs E/W.
	gpsRationals := func(v float64, axis byte) (string, [3][2]uint32) {
		var neg string
		switch axis {
		case 'N':
			neg = "S"
		default:
			neg = "W"
		}
		pos := string(axis)
		if axis != 'N' {
			pos = "E"
		}
		if v < 0 {
			v = -v
			pos = neg
		}
		deg := math.Floor(v)
		min := math.Floor((v - deg) * 60)
		sec := math.Round(((v-deg)*60 - min) * 60 * 10000)
		return pos, [3][2]uint32{{uint32(deg), 1}, {uint32(min), 1}, {uint32(sec), 10000}}
	}

	ifd0, exifIFD, gpsIFD := []entry{}, []entry{}, []entry{}

	if o.Description != "" {
		ifd0 = append(ifd0, asciiEntry(0x010E, o.Description))
	}
	if o.Make != "" {
		ifd0 = append(ifd0, asciiEntry(0x010F, o.Make))
	}
	if o.Model != "" {
		ifd0 = append(ifd0, asciiEntry(0x0110, o.Model))
	}
	if o.Orientation != 0 {
		ifd0 = append(ifd0, shortEntry(0x0112, o.Orientation))
	}
	if o.Rating > 0 && o.Rating <= 5 {
		ifd0 = append(ifd0, shortEntry(0x4746, uint16(o.Rating)))
	}
	if o.DateTimeOriginal != nil {
		exifIFD = append(exifIFD, asciiEntry(0x9003, o.DateTimeOriginal.Format("2006:01:02 15:04:05")))
	}
	if o.Width > 0 {
		exifIFD = append(exifIFD, longEntry(0xA002, uint32(o.Width)))
	}
	if o.Height > 0 {
		exifIFD = append(exifIFD, longEntry(0xA003, uint32(o.Height)))
	}
	if o.LensModel != "" {
		exifIFD = append(exifIFD, asciiEntry(0xA434, o.LensModel))
	}
	if o.Latitude != nil {
		ref, rats := gpsRationals(*o.Latitude, 'N')
		gpsIFD = append(gpsIFD, asciiEntry(0x0001, ref), rational3Entry(0x0002, rats))
	}
	if o.Longitude != nil {
		ref, rats := gpsRationals(*o.Longitude, 'E')
		gpsIFD = append(gpsIFD, asciiEntry(0x0003, ref), rational3Entry(0x0004, rats))
	}

	ifdSize := func(n int) int {
		if n == 0 {
			return 0
		}
		return 2 + 12*n + 4
	}
	// Sub-IFD pointers must be part of IFD0 before its size is measured,
	// otherwise the emitted offsets would point into the middle of IFD0.
	if len(exifIFD) > 0 {
		ifd0 = append(ifd0, longEntry(0x8769, 0)) // placeholder, patched below
	}
	if len(gpsIFD) > 0 {
		ifd0 = append(ifd0, longEntry(0x8825, 0)) // placeholder, patched below
	}

	ifd0Off := uint32(8)
	exifOff := ifd0Off + uint32(ifdSize(len(ifd0)))
	gpsOff := exifOff + uint32(ifdSize(len(exifIFD)))
	heapOff := gpsOff + uint32(ifdSize(len(gpsIFD)))

	for i := range ifd0 {
		switch ifd0[i].tag {
		case 0x8769:
			ifd0[i] = longEntry(0x8769, exifOff)
		case 0x8825:
			ifd0[i] = longEntry(0x8825, gpsOff)
		}
	}

	// Allocate heap space for oversized values in declaration order.
	assign := func(entries []entry) {
		for i := range entries {
			if len(entries[i].payload) > 4 {
				entries[i].heapOff = heapOff
				heapOff += uint32(len(entries[i].payload))
			}
		}
	}
	assign(ifd0)
	assign(exifIFD)
	assign(gpsIFD)

	buf := new(bytes.Buffer)
	write := func(b ...byte) { buf.Write(b) }
	put16 := func(v uint16) { write(enc16(v)...) }
	put32 := func(v uint32) { write(enc32(v)...) }

	if le {
		write('I', 'I')
	} else {
		write('M', 'M')
	}
	put16(42)
	put32(8)

	writeIFD := func(entries []entry) {
		put16(uint16(len(entries)))
		for _, e := range entries {
			put16(e.tag)
			put16(e.typ)
			put32(e.count)
			if len(e.payload) <= 4 {
				var inline [4]byte
				copy(inline[:], e.payload)
				write(inline[:]...)
			} else {
				put32(e.heapOff)
			}
		}
		put32(0) // no next IFD
	}

	var heap bytes.Buffer
	collectHeap := func(entries []entry) {
		for _, e := range entries {
			if len(e.payload) > 4 {
				heap.Write(e.payload)
			}
		}
	}

	writeIFD(ifd0)
	if len(exifIFD) > 0 {
		writeIFD(exifIFD)
	}
	if len(gpsIFD) > 0 {
		writeIFD(gpsIFD)
	}
	collectHeap(ifd0)
	collectHeap(exifIFD)
	collectHeap(gpsIFD)
	buf.Write(heap.Bytes())
	return buf.Bytes()
}
