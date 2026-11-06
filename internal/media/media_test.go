package media

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These tests run ffmpeg, and skip where it is not installed. CI installs it.
func requireFFmpeg(t *testing.T) {
	t.Helper()
	for _, name := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Skipf("%s is not installed", name)
		}
	}
}

// makeClip writes a test video of the size and length, with a tone when audio is true.
func makeClip(t *testing.T, width, height, seconds int, audio bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "clip.mp4")
	args := []string{"-hide_banner", "-nostdin", "-y", "-f", "lavfi", "-i", fmt.Sprintf("testsrc2=size=%dx%d:rate=30:duration=%d", width, height, seconds)}
	if audio {
		args = append(args, "-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=440:duration=%d", seconds))
	}
	args = append(args, "-c:v", "libx264", "-pix_fmt", "yuv420p")
	if audio {
		args = append(args, "-c:a", "aac")
	}
	if output, err := exec.Command("ffmpeg", append(args, path)...).CombinedOutput(); err != nil {
		t.Fatalf("make a test clip: %v\n%s", err, output)
	}
	return path
}

func TestProbeFile(t *testing.T) {
	requireFFmpeg(t)
	probe, err := ProbeFile(context.Background(), "ffprobe", makeClip(t, 640, 360, 3, true))
	if err != nil {
		t.Fatal(err)
	}
	if probe.Width != 640 || probe.Height != 360 || !probe.HasAudio || probe.Duration < 2.9 || probe.Duration > 3.2 {
		t.Errorf("probe %+v", probe)
	}
}

func TestProbeRefusesFilesWithoutVideo(t *testing.T) {
	requireFFmpeg(t)
	// Uploads are stored as "source", without the extension that ffprobe would also go by.
	path := filepath.Join(t.TempDir(), "source")
	os.WriteFile(path, []byte("these bytes are not a video, nor any other kind of media"), 0o644)
	if _, err := ProbeFile(context.Background(), "ffprobe", path); err == nil {
		t.Error("a text file passed as a video")
	}
}

func TestEncodePipeline(t *testing.T) {
	requireFFmpeg(t)
	for _, audio := range []bool{true, false} {
		t.Run(fmt.Sprintf("audio=%v", audio), func(t *testing.T) {
			ctx := context.Background()
			source := makeClip(t, 1280, 720, 9, audio)
			probe, err := ProbeFile(ctx, "ffprobe", source)
			if err != nil {
				t.Fatal(err)
			}
			rungs := Ladder(probe.Width, probe.Height)
			dir := t.TempDir()
			hls := filepath.Join(dir, "hls")
			var progress float64
			err = EncodeHLS(ctx, EncodeOptions{
				FFmpeg: "ffmpeg", Source: source, Probe: probe, Rungs: rungs, Threads: 2, OutDir: hls,
				Progress: func(fraction float64) { progress = fraction },
			})
			if err != nil {
				t.Fatal(err)
			}
			if progress < 0.9 {
				t.Errorf("the last progress was %g", progress)
			}
			master := readFile(t, filepath.Join(hls, MasterPlaylist))
			if got := strings.Count(master, "#EXT-X-STREAM-INF"); got != len(rungs) {
				t.Errorf("the master playlist lists %d qualities, want %d:\n%s", got, len(rungs), master)
			}
			for _, rung := range rungs {
				playlist := readFile(t, filepath.Join(hls, VariantPlaylist(rung.Name)))
				if !strings.Contains(master, VariantPlaylist(rung.Name)) || !strings.Contains(playlist, "#EXT-X-ENDLIST") {
					t.Errorf("quality %s:\n%s", rung.Name, playlist)
				}
				first := filepath.Join(hls, rung.Name+"_00000.ts")
				if _, err := os.Stat(first); err != nil {
					t.Error(err)
				}
				if !strings.Contains(playlist, rung.Name+"_00000.ts") {
					t.Errorf("the playlist of %s does not name its first segment:\n%s", rung.Name, playlist)
				}
			}

			if err := Poster(ctx, "ffmpeg", source, probe.Duration, filepath.Join(dir, "poster.jpg")); err != nil {
				t.Fatal(err)
			}
			smallest := filepath.Join(hls, VariantPlaylist(rungs[len(rungs)-1].Name))
			if err := Storyboard(ctx, "ffmpeg", smallest, probe.Duration, dir); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(dir, "storyboard.jpg")); err != nil {
				t.Error(err)
			}
			vtt := readFile(t, filepath.Join(dir, "storyboard.vtt"))
			// A 9 second video gets a picture every 2 seconds: 5 of them.
			if got := strings.Count(vtt, "storyboard.jpg#xywh="); got != 5 {
				t.Errorf("the storyboard has %d cues, want 5:\n%s", got, vtt)
			}
			// The fifth picture starts at 8 seconds, in the fifth place of the first row.
			if !strings.Contains(vtt, "\n00:00:08.000 --> ") || !strings.HasSuffix(strings.TrimSpace(vtt), "storyboard.jpg#xywh=640,0,160,90") {
				t.Errorf("the last cue is wrong:\n%s", vtt)
			}
		})
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
