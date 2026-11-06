package media

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
)

// Poster writes a still of the video at a tenth of its length as a JPEG, no wider than 1280 pixels.
func Poster(ctx context.Context, ffmpeg, source string, duration float64, outPath string) error {
	at := math.Min(math.Max(duration*0.1, 0), math.Max(duration-0.1, 0))
	return run(ctx, ffmpeg, []string{
		"-hide_banner", "-nostdin", "-y",
		"-ss", fmt.Sprintf("%.3f", at), "-i", source,
		"-frames:v", "1", "-vf", "scale='min(1280,iw)':-2", "-q:v", "3",
		outPath,
	}, nil)
}

// The size of each picture of the storyboard, and the pictures in each row of its sheet.
const (
	tileWidth  = 160
	tileHeight = 90
	tileCols   = 10
	// maxTiles keeps the sheet of a long video a few hundred kilobytes.
	maxTiles = 100
)

// Storyboard writes a sheet of small pictures of the video, and a WebVTT file that
// tells players which part of the sheet shows each moment, for previews on the seek
// bar. It reads the smallest quality, which decodes far faster than the source.
func Storyboard(ctx context.Context, ffmpeg, input string, duration float64, dir string) error {
	interval := math.Max(2, math.Ceil(duration/maxTiles))
	tiles := int(math.Ceil(duration / interval))
	rows := (tiles + tileCols - 1) / tileCols
	sheet := filepath.Join(dir, "storyboard.jpg")
	filter := fmt.Sprintf("fps=1/%g,scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,tile=%dx%d",
		interval, tileWidth, tileHeight, tileWidth, tileHeight, tileCols, max(rows, 1))
	if err := run(ctx, ffmpeg, []string{
		"-hide_banner", "-nostdin", "-y", "-i", input,
		"-vf", filter, "-frames:v", "1", "-q:v", "4", sheet,
	}, nil); err != nil {
		return err
	}
	var vtt strings.Builder
	vtt.WriteString("WEBVTT\n")
	for i := 0; i < tiles; i++ {
		start := float64(i) * interval
		end := math.Min(float64(i+1)*interval, duration)
		fmt.Fprintf(&vtt, "\n%s --> %s\nstoryboard.jpg#xywh=%d,%d,%d,%d\n",
			timestamp(start), timestamp(end), (i%tileCols)*tileWidth, (i/tileCols)*tileHeight, tileWidth, tileHeight)
	}
	return os.WriteFile(filepath.Join(dir, "storyboard.vtt"), []byte(vtt.String()), 0o644)
}

// timestamp writes seconds as a WebVTT time, such as 01:02:03.500.
func timestamp(seconds float64) string {
	total := int(math.Round(seconds * 1000))
	return fmt.Sprintf("%02d:%02d:%02d.%03d", total/3_600_000, total/60_000%60, total/1000%60, total%1000)
}
