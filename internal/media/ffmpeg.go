// ffmpeg.go — video frame extraction via the ffmpeg CLI, the same
// tool the upstream server drives through fluent-ffmpeg. When ffmpeg is
// not installed, video thumbnails degrade gracefully (no renditions;
// the thumbnail endpoint falls through per asset type).
package media

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

var (
	ffmpegOnce   sync.Once
	ffmpegExists bool
)

// HasFFmpeg reports whether the ffmpeg binary is on PATH (checked once).
func HasFFmpeg() bool {
	ffmpegOnce.Do(func() {
		_, err := exec.LookPath("ffmpeg")
		ffmpegExists = err == nil
	})
	return ffmpegExists
}

// ExtractFrame decodes the frame at atSeconds into a JPEG whose longest
// edge is at most maxEdge (bounding-box scale, aspect preserved).
func ExtractFrame(path string, atSeconds float64, maxEdge int) ([]byte, error) {
	if !HasFFmpeg() {
		return nil, fmt.Errorf("ffmpeg not installed")
	}
	if atSeconds < 0 {
		atSeconds = 0
	}
	if maxEdge <= 0 {
		maxEdge = PreviewMax
	}
	// Commas are filter-graph separators; escape them inside the scale
	// expression arguments.
	expr := fmt.Sprintf(
		"scale='trunc(min(1\\,min(%d/iw\\,%d/ih))*iw/2)*2':'trunc(min(1\\,min(%d/iw\\,%d/ih))*ih/2)*2'",
		maxEdge, maxEdge, maxEdge, maxEdge)

	// -ss before -i is a fast seek; image2 + pipe keeps everything in
	// memory without temp files.
	cmd := exec.Command("ffmpeg",
		"-hide_banner", "-loglevel", "error",
		"-ss", fmt.Sprintf("%.3f", atSeconds),
		"-i", path,
		"-frames:v", "1",
		"-vf", expr,
		"-f", "image2", "-c:v", "mjpeg", "-q:v", "3",
		"pipe:1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > 200 {
			msg = msg[:200]
		}
		return nil, fmt.Errorf("ffmpeg frame extraction: %w: %s", err, msg)
	}
	if stdout.Len() == 0 {
		return nil, fmt.Errorf("ffmpeg produced no frame")
	}
	return stdout.Bytes(), nil
}
