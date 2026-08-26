package videometa

import (
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"
)

// ErrNoProbe is returned when neither the pure-Go parser nor ffprobe can
// handle the file.
var ErrNoProbe = fmt.Errorf("no video probe available")

// HasFFprobe reports whether the ffmpeg suite is installed.
func HasFFprobe() bool {
	_, err := exec.LookPath("ffprobe")
	return err == nil
}

// probeFFprobe shells out to ffprobe for containers the native MP4
// parser cannot read (webm, mkv, avi, ...).
func probeFFprobe(path string) (*Info, error) {
	if !HasFFprobe() {
		return nil, ErrNoProbe
	}
	out, err := exec.Command("ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_format", "-show_streams",
		path).Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe: %w", err)
	}

	var doc struct {
		Streams []struct {
			CodecType string  `json:"codec_type"`
			CodecName string  `json:"codec_name"`
			Width     int     `json:"width"`
			Height    int     `json:"height"`
			FPS       string  `json:"r_frame_rate"`
			Duration  float64 `json:"duration"`
			Rotation  int     `json:"rotate"`
			Tags      struct {
				Rotate string `json:"rotate"`
			} `json:"tags"`
			SideData []struct {
				Rotation float64 `json:"rotation"`
			} `json:"side_data_list"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		return nil, fmt.Errorf("ffprobe json: %w", err)
	}

	info := &Info{}
	if d, err := strconv.ParseFloat(doc.Format.Duration, 64); err == nil && d > 0 {
		info.DurationMs = int64(math.Round(d * 1000))
	}
	for _, s := range doc.Streams {
		switch s.CodecType {
		case "video":
			if info.Width == 0 && s.Width > 0 {
				info.Width, info.Height = s.Width, s.Height
			}
			if info.VideoCodec == "" {
				info.VideoCodec = s.CodecName
			}
			if info.FPS == 0 {
				info.FPS = parseRatio(s.FPS)
			}
			if s.Rotation != 0 {
				info.RotationDeg = normalizeRotation(s.Rotation)
			}
			if v, err := strconv.Atoi(s.Tags.Rotate); err == nil && v != 0 {
				info.RotationDeg = normalizeRotation(v)
			}
			for _, sd := range s.SideData {
				if sd.Rotation != 0 {
					info.RotationDeg = normalizeRotation(int(-sd.Rotation))
				}
			}
			if info.DurationMs == 0 && s.Duration > 0 {
				info.DurationMs = int64(math.Round(s.Duration * 1000))
			}
		case "audio":
			if info.AudioCodec == "" {
				info.AudioCodec = s.CodecName
			}
		}
	}
	if info.Width == 0 && info.DurationMs == 0 {
		return nil, fmt.Errorf("ffprobe returned no usable data")
	}
	if info.RotationDeg == 90 || info.RotationDeg == 270 {
		info.Width, info.Height = info.Height, info.Width
	}
	return info, nil
}

func parseRatio(s string) float64 {
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 {
		return 0
	}
	num, err1 := strconv.ParseFloat(parts[0], 64)
	den, err2 := strconv.ParseFloat(parts[1], 64)
	if err1 != nil || err2 != nil || den == 0 {
		return 0
	}
	return num / den
}

func normalizeRotation(deg int) int {
	deg %= 360
	if deg < 0 {
		deg += 360
	}
	return deg
}
