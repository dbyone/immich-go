// Package exif is a pure-Go EXIF reader for JPEG and TIFF files. It
// replaces the upstream exiftool dependency for the tags that feed the
// asset metadata: camera make/model/lens, original capture date, pixel
// dimensions, orientation, description, rating and GPS coordinates.
//
// The parser is deliberately defensive: malformed or truncated input
// yields partial data instead of panics, and files without EXIF return an
// empty Data with a nil error.
package exif

import (
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

// Data carries the tags the server persists in asset_exifs.
type Data struct {
	Make         string
	Model        string
	LensModel    string
	Description  string
	Rating       *int
	Orientation  int // EXIF orientation 1-8; 0 when absent
	Width        int // PixelXDimension
	Height       int // PixelYDimension
	DateTimeOriginal *time.Time
	OffsetTimeOriginal string // e.g. "+08:00"
	Latitude     *float64
	Longitude    *float64
}

// ParseFile reads the EXIF block of a file on disk.
func ParseFile(path string) (*Data, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	return Parse(f, fi.Size())
}

// Parse reads the EXIF block from r (JPEG stream or TIFF container).
func Parse(r io.ReaderAt, size int64) (*Data, error) {
	if size < 4 {
		return &Data{}, nil
	}
	head := make([]byte, 2)
	if _, err := r.ReadAt(head, 0); err != nil {
		return nil, err
	}
	switch {
	case head[0] == 0xFF && head[1] == 0xD8:
		return parseJPEG(r, size)
	case head[0] == 'I' && head[1] == 'I', head[0] == 'M' && head[1] == 'M':
		return parseContainer(r, size, 0)
	default:
		// Other containers (PNG/HEIC/...) carry no JPEG-style EXIF here.
		return &Data{}, nil
	}
}

// parseJPEG walks the segment stream until SOS looking for an APP1
// "Exif\0\0" payload.
func parseJPEG(r io.ReaderAt, size int64) (*Data, error) {
	off := int64(2)
	buf := make([]byte, 4)
	for off+4 <= size {
		if _, err := r.ReadAt(buf, off); err != nil {
			return nil, err
		}
		if buf[0] != 0xFF {
			return &Data{}, nil // desynchronized; no usable EXIF
		}
		marker := buf[1]
		// Standalone markers without a length field.
		if marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7) {
			off += 2
			continue
		}
		if marker == 0xD8 || marker == 0xD9 || marker == 0xDA {
			return &Data{}, nil // start of scan / end of image
		}
		length := int64(buf[2])<<8 | int64(buf[3])
		if length < 2 || off+2+length > size {
			return &Data{}, nil
		}
		if marker == 0xE1 && length > 2 {
			payload := make([]byte, length-2)
			if _, err := r.ReadAt(payload, off+4); err != nil {
				return nil, err
			}
			if len(payload) > 6 && string(payload[:6]) == "Exif\x00\x00" {
				return parseBytes(payload[6:])
			}
		}
		off += 2 + length
	}
	return &Data{}, nil
}

// parseContainer parses a plain TIFF file (the TIFF structure IS the
// container). The header is read into memory up to a bounded prefix.
func parseContainer(r io.ReaderAt, size int64, base int64) (*Data, error) {
	const maxBlock = 64 << 20 // EXIF blocks are far smaller; guards huge TIFFs
	n := size - base
	if n > maxBlock {
		n = maxBlock
	}
	b := make([]byte, n)
	if _, err := r.ReadAt(b, base); err != nil && err != io.EOF {
		return nil, err
	}
	return parseBytes(b)
}

// parseBytes parses a raw TIFF block (II*/MM* header + IFDs).
func parseBytes(b []byte) (*Data, error) {
	if len(b) < 8 {
		return &Data{}, nil
	}
	var le bool
	switch {
	case b[0] == 'I' && b[1] == 'I':
		le = true
	case b[0] == 'M' && b[1] == 'M':
		le = false
	default:
		return &Data{}, nil
	}
	t := &tiff{b: b, le: le}
	if t.u16(2) != 42 {
		return &Data{}, nil
	}
	ifd0 := int(t.u32(4))
	if ifd0 <= 0 || ifd0 >= len(b) {
		return &Data{}, nil
	}

	d := &Data{}
	exifOff, gpsOff := 0, 0
	t.eachEntry(ifd0, func(tag, typ uint16, count uint32, val []byte) {
		switch tag {
		case 0x010F:
			d.Make = ascii(val)
		case 0x0110:
			d.Model = ascii(val)
		case 0x0112:
			if v, ok := t.firstInt(typ, count, val); ok {
				d.Orientation = int(v)
			}
		case 0x0132, 0x9004: // DateTime / CreateDate (fallback source)
			if d.DateTimeOriginal == nil {
				if ts := parseExifTime(ascii(val), ""); ts != nil {
					d.DateTimeOriginal = ts
				}
			}
		case 0x010E:
			d.Description = ascii(val)
		case 0x4746: // Microsoft Rating
			if v, ok := t.firstInt(typ, count, val); ok && v >= 0 && v <= 5 {
				r := int(v)
				d.Rating = &r
			}
		case 0x8769:
			if v, ok := t.firstInt(typ, count, val); ok {
				exifOff = int(v)
			}
		case 0x8825:
			if v, ok := t.firstInt(typ, count, val); ok {
				gpsOff = int(v)
			}
		}
	})

	var subSec string
	var dtString string
	if exifOff > 0 && exifOff < len(b) {
		t.eachEntry(exifOff, func(tag, typ uint16, count uint32, val []byte) {
			switch tag {
			case 0x9003:
				dtString = ascii(val)
			case 0x9291: // SubSecTimeOriginal
				subSec = strings.TrimRight(ascii(val), " \x00")
			case 0x9011:
				d.OffsetTimeOriginal = ascii(val)
			case 0xA002:
				if v, ok := t.firstInt(typ, count, val); ok {
					d.Width = int(v)
				}
			case 0xA003:
				if v, ok := t.firstInt(typ, count, val); ok {
					d.Height = int(v)
				}
			case 0xA434:
				d.LensModel = ascii(val)
			case 0x9286: // UserComment (undefined, 8-byte charset prefix)
				s := userComment(val)
				if s != "" && d.Description == "" {
					d.Description = s
				}
			}
		})
	}
	if ts := parseExifTime(dtString, subSec); ts != nil {
		d.DateTimeOriginal = ts
	}

	if gpsOff > 0 && gpsOff < len(b) {
		latRef, lonRef := "", ""
		var latD, latM, latS, lonD, lonM, lonS float64
		haveLat, haveLon := false, false
		t.eachEntry(gpsOff, func(tag, typ uint16, count uint32, val []byte) {
			switch tag {
			case 0x0001:
				latRef = ascii(val)
			case 0x0002:
				if dms, ok := t.dms(val); ok {
					latD, latM, latS = dms[0], dms[1], dms[2]
					haveLat = true
				}
			case 0x0003:
				lonRef = ascii(val)
			case 0x0004:
				if dms, ok := t.dms(val); ok {
					lonD, lonM, lonS = dms[0], dms[1], dms[2]
					haveLon = true
				}
			}
		})
		if haveLat {
			v := latD + latM/60 + latS/3600
			if strings.EqualFold(latRef, "S") {
				v = -v
			}
			d.Latitude = &v
		}
		if haveLon {
			v := lonD + lonM/60 + lonS/3600
			if strings.EqualFold(lonRef, "W") {
				v = -v
			}
			d.Longitude = &v
		}
	}
	return d, nil
}

// ---- TIFF primitive access ----

type tiff struct {
	b  []byte
	le bool
}

func (t *tiff) u16(off int) uint16 {
	if off+2 > len(t.b) {
		return 0
	}
	if t.le {
		return uint16(t.b[off]) | uint16(t.b[off+1])<<8
	}
	return uint16(t.b[off])<<8 | uint16(t.b[off+1])
}

func (t *tiff) u32(off int) uint32 {
	if off+4 > len(t.b) {
		return 0
	}
	if t.le {
		return uint32(t.b[off]) | uint32(t.b[off+1])<<8 | uint32(t.b[off+2])<<16 | uint32(t.b[off+3])<<24
	}
	return uint32(t.b[off])<<24 | uint32(t.b[off+1])<<16 | uint32(t.b[off+2])<<8 | uint32(t.b[off+3])
}

var typeSize = map[uint16]int{1: 1, 2: 1, 3: 2, 4: 4, 5: 8, 6: 1, 7: 1, 8: 2, 9: 4, 10: 8, 11: 4, 12: 8}

// eachEntry iterates the IFD at off, invoking fn with the decoded value
// bytes of every entry (inline values copied out, offsets dereferenced).
func (t *tiff) eachEntry(off int, fn func(tag, typ uint16, count uint32, val []byte)) {
	n := int(t.u16(off))
	if n <= 0 || n > 512 { // sanity cap against hostile files
		return
	}
	for i := 0; i < n; i++ {
		e := off + 2 + i*12
		if e+12 > len(t.b) {
			return
		}
		tag := t.u16(e)
		typ := t.u16(e + 2)
		count := t.u32(e + 4)
		size, ok := typeSize[typ]
		if !ok {
			continue
		}
		total := int64(size) * int64(count) // int64: count can be hostile
		if total < 0 || total > int64(len(t.b)) {
			continue
		}
		var val []byte
		if total <= 4 {
			val = t.b[e+8 : e+8+int(total)]
		} else {
			voff := int64(t.u32(e + 8))
			if voff <= 0 || voff+total > int64(len(t.b)) {
				continue
			}
			val = t.b[int(voff) : int(voff)+int(total)]
		}
		fn(tag, typ, count, val)
	}
}

// firstInt decodes the leading integer of a BYTE/SHORT/LONG entry.
func (t *tiff) firstInt(typ uint16, count uint32, val []byte) (uint32, bool) {
	if count == 0 || len(val) == 0 {
		return 0, false
	}
	switch typ {
	case 1, 6: // (U)BYTE
		return uint32(val[0]), true
	case 3, 8: // (U)SHORT
		if len(val) < 2 {
			return 0, false
		}
		if t.le {
			return uint32(val[0]) | uint32(val[1])<<8, true
		}
		return uint32(val[0])<<8 | uint32(val[1]), true
	case 4, 9: // (S)LONG
		if len(val) < 4 {
			return 0, false
		}
		if t.le {
			return uint32(val[0]) | uint32(val[1])<<8 | uint32(val[2])<<16 | uint32(val[3])<<24, true
		}
		return uint32(val[0])<<24 | uint32(val[1])<<16 | uint32(val[2])<<8 | uint32(val[3]), true
	default:
		return 0, false
	}
}

// dms decodes the three RATIONALs of a GPS coordinate.
func (t *tiff) dms(val []byte) ([3]float64, bool) {
	var out [3]float64
	if len(val) != 24 {
		return out, false
	}
	for i := 0; i < 3; i++ {
		num, den := t.rational(val[i*8 : i*8+8])
		if den == 0 {
			return out, false
		}
		out[i] = num / den
	}
	return out, true
}

func (t *tiff) rational(v []byte) (float64, float64) {
	if len(v) < 8 {
		return 0, 0
	}
	var num, den uint32
	if t.le {
		num = uint32(v[0]) | uint32(v[1])<<8 | uint32(v[2])<<16 | uint32(v[3])<<24
		den = uint32(v[4]) | uint32(v[5])<<8 | uint32(v[6])<<16 | uint32(v[7])<<24
	} else {
		num = uint32(v[0])<<24 | uint32(v[1])<<16 | uint32(v[2])<<8 | uint32(v[3])
		den = uint32(v[4])<<24 | uint32(v[5])<<16 | uint32(v[6])<<8 | uint32(v[7])
	}
	return float64(num), float64(den)
}

// ---- value decoding helpers ----

func ascii(b []byte) string {
	if i := indexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return strings.TrimSpace(string(b))
}

func indexByte(b []byte, c byte) int {
	for i, v := range b {
		if v == c {
			return i
		}
	}
	return -1
}

// userComment strips the 8-byte character-code prefix mandated by the
// EXIF spec when it looks like one of the known codes.
func userComment(b []byte) string {
	if len(b) > 8 {
		prefix := string(b[:8])
		for _, known := range []string{"ASCII", "UNICODE", "JIS", "\x00\x00\x00\x00\x00\x00\x00\x00"} {
			if len(prefix) >= len(known) && prefix[:len(known)] == known {
				return ascii(b[8:])
			}
		}
	}
	return ascii(b)
}

// parseExifTime parses "2006:01:02 15:04:05" (optionally with a
// sub-second suffix) into a naive local timestamp.
func parseExifTime(s, subSec string) *time.Time {
	s = strings.TrimSpace(s)
	if len(s) < 19 {
		return nil
	}
	// Some writers emit "2006-01-02" style separators.
	normalized := strings.ReplaceAll(s[:19], "-", ":")
	t, err := time.ParseInLocation("2006:01:02 15:04:05", normalized, time.UTC)
	if err != nil {
		return nil
	}
	if subSec != "" {
		digits := strings.TrimLeft(subSec, "0")
		if digits != "" {
			for len(digits) < 9 {
				digits += "0"
			}
			if nanos, err := strconv.Atoi(digits[:9]); err == nil {
				t = t.Add(time.Duration(nanos) * time.Nanosecond)
			}
		}
	}
	return &t
}
