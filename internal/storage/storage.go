// Package storage manages the on-disk media layout. The folder structure
// mirrors the Immich conventions: upload/{userId}/{a}/{b}/{uuid}.ext for
// incoming files and thumbs/{ownerId}/{a}/{b}/{assetId}-{variant}.jpeg for
// generated renditions.
package storage

import (
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"immich-go/internal/domain"
)

type Storage struct {
	root string
}

func New(root string) (*Storage, error) {
	if root == "" {
		return nil, errors.New("storage root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	for _, folder := range []string{"upload", "library", "thumbs", "encoded-video", "profile"} {
		if err := os.MkdirAll(filepath.Join(abs, folder), 0o755); err != nil {
			return nil, err
		}
	}
	return &Storage{root: abs}, nil
}

func (s *Storage) Root() string { return s.root }

func nestedDir(base, id string) string {
	return filepath.Join(base, id[:2], id[2:4])
}

func sanitizeExt(ext string) string {
	ext = strings.ToLower(strings.TrimPrefix(ext, "."))
	if ext == "" || len(ext) > 5 || strings.ContainsAny(ext, `/\.`) {
		return "bin"
	}
	return ext
}

// SaveUpload streams a multipart file part into the upload folder while
// computing the SHA-1 checksum, returning the storage path and checksum
// (both raw digest and base64 form).
func (s *Storage) SaveUpload(r io.Reader, userID, id, ext string) (path string, sum []byte, sumB64 string, size int64, err error) {
	dir := nestedDir(filepath.Join(s.root, "upload", userID), id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", nil, "", 0, err
	}
	path = filepath.Join(dir, id+"."+sanitizeExt(ext))

	f, err := os.Create(path)
	if err != nil {
		return "", nil, "", 0, err
	}
	defer f.Close()

	h := sha1.New()
	n, err := io.Copy(io.MultiWriter(f, h), r)
	if err != nil {
		os.Remove(path)
		return "", nil, "", 0, err
	}
	sum = h.Sum(nil)
	return path, sum, base64.StdEncoding.EncodeToString(sum), n, nil
}

// WriteFile stores a generated rendition under the thumbs tree.
func (s *Storage) WriteThumb(ownerID, assetID, variant string, data []byte) (string, error) {
	dir := nestedDir(filepath.Join(s.root, "thumbs", ownerID), assetID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	p := filepath.Join(dir, assetID+"-"+variant+".jpeg")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		return "", err
	}
	return p, nil
}

// Open returns a reader over a stored path.
func (s *Storage) Open(path string) (io.ReadSeekCloser, error) {
	return os.Open(path)
}

// Remove deletes a stored file, ignoring missing files.
func (s *Storage) Remove(path string) {
	if path != "" {
		os.Remove(path)
	}
}

// MimeTypeByAsset guesses the content type served with a file response.
func MimeTypeByAsset(a *domain.Asset) string {
	switch strings.ToLower(strings.TrimPrefix(filepath.Ext(a.OriginalPath), ".")) {
	case "jpg", "jpeg":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	case "avif":
		return "image/avif"
	case "heic", "heif":
		return "image/heic"
	case "tiff":
		return "image/tiff"
	case "mp4":
		return "video/mp4"
	case "mov":
		return "video/quicktime"
	case "webm":
		return "video/webm"
	case "avi":
		return "video/x-msvideo"
	case "mp3":
		return "audio/mpeg"
	case "wav":
		return "audio/wav"
	case "aac":
		return "audio/aac"
	case "flac":
		return "audio/flac"
	case "ogg":
		return "audio/ogg"
	default:
		return "application/octet-stream"
	}
}

// AssetTypeFromMime maps an upload to the Immich asset type enum.
func AssetTypeFromMime(mime, filename string) string {
	m := strings.ToLower(mime)
	ext := strings.ToLower(filepath.Ext(filename))
	switch {
	case strings.HasPrefix(m, "image/"), strings.Contains(".jpg.jpeg.png.gif.webp.avif.heic.heif.tiff.bmp.dng.rw2.cr2.nef.arw.", ext+"."):
		return domain.AssetImage
	case strings.HasPrefix(m, "video/"), strings.Contains(".mp4.mov.webm.avi.mkv.m4v.mpg.mpeg.3gp.wmv.mts.", ext+"."):
		return domain.AssetVideo
	case strings.HasPrefix(m, "audio/"), strings.Contains(".mp3.wav.aac.flac.ogg.m4a.wma.", ext+"."):
		return domain.AssetAudio
	default:
		return domain.AssetOther
	}
}
