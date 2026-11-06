// Package media turns an uploaded video into what deneb serves: an HLS stream in
// several qualities, a poster, a storyboard for the seek bar, and captions. The
// work runs in ffmpeg and whisper.cpp.
package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"
)

// Probe describes the source of a video.
type Probe struct {
	// Duration in seconds.
	Duration float64
	// Width and Height as the video is displayed, after any rotation.
	Width  int
	Height int
	// HasAudio tells whether the source has a sound track.
	HasAudio bool
}

type probeOutput struct {
	Streams []struct {
		CodecType string            `json:"codec_type"`
		Width     int               `json:"width"`
		Height    int               `json:"height"`
		Duration  string            `json:"duration"`
		Tags      map[string]string `json:"tags"`
		// SampleAspectRatio is the shape of a pixel, such as "4:3" for HDV, whose
		// 1440 stored columns show 1920 wide.
		SampleAspectRatio string `json:"sample_aspect_ratio"`
		// AttachedPic marks the cover art of an audio file, which is a picture, not a video.
		Disposition struct {
			AttachedPic int `json:"attached_pic"`
		} `json:"disposition"`
		SideData []struct {
			Rotation float64 `json:"rotation"`
		} `json:"side_data_list"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

// ProbeFile reads the duration, size, and tracks of a video with ffprobe.
func ProbeFile(ctx context.Context, ffprobe, path string) (Probe, error) {
	cmd := exec.CommandContext(ctx, ffprobe, "-v", "error", "-print_format", "json", "-show_format", "-show_streams", path)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return Probe{}, fmt.Errorf("ffprobe: %s", lastLines(stderr.String(), 3, err))
	}
	var output probeOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		return Probe{}, fmt.Errorf("read the output of ffprobe: %w", err)
	}
	var probe Probe
	found := false
	for _, stream := range output.Streams {
		switch stream.CodecType {
		case "video":
			if found || stream.Width == 0 || stream.Height == 0 || stream.Disposition.AttachedPic == 1 {
				continue
			}
			found = true
			probe.Width, probe.Height = stream.Width, stream.Height
			if num, den, ok := ratio(stream.SampleAspectRatio); ok && num != den {
				probe.Width = even(int(math.Round(float64(stream.Width) * float64(num) / float64(den))))
			}
			if quarterTurn(stream.Tags["rotate"], stream.SideData) {
				probe.Width, probe.Height = probe.Height, probe.Width
			}
			probe.Duration = parseSeconds(stream.Duration)
		case "audio":
			probe.HasAudio = true
		}
	}
	if !found {
		return Probe{}, errors.New("the file has no video track")
	}
	if duration := parseSeconds(output.Format.Duration); duration > 0 {
		probe.Duration = duration
	}
	if probe.Duration <= 0 {
		return Probe{}, errors.New("ffprobe found no duration")
	}
	return probe, nil
}

// quarterTurn tells whether a phone recorded the video turned by 90 or 270 degrees,
// which swaps its displayed width and height.
func quarterTurn(rotateTag string, sideData []struct {
	Rotation float64 `json:"rotation"`
}) bool {
	rotation := 0.0
	if value, err := strconv.ParseFloat(rotateTag, 64); err == nil {
		rotation = value
	}
	for _, data := range sideData {
		if data.Rotation != 0 {
			rotation = data.Rotation
		}
	}
	turns := math.Mod(math.Abs(rotation), 180)
	return turns > 45 && turns < 135
}

// ratio reads a ratio such as "4:3". ffprobe writes "0:1" or "N/A" when it does not know.
func ratio(value string) (int, int, bool) {
	numText, denText, found := strings.Cut(value, ":")
	num, numErr := strconv.Atoi(numText)
	den, denErr := strconv.Atoi(denText)
	if !found || numErr != nil || denErr != nil || num <= 0 || den <= 0 {
		return 0, 0, false
	}
	return num, den, true
}

func parseSeconds(value string) float64 {
	seconds, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return 0
	}
	return seconds
}

// lastLines keeps the end of the output of a program, where it explains why it failed.
func lastLines(output string, count int, err error) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) > count {
		lines = lines[len(lines)-count:]
	}
	text := strings.TrimSpace(strings.Join(lines, " | "))
	if text == "" {
		return err.Error()
	}
	return text
}
