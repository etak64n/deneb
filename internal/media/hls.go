package media

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// SegmentSeconds is the length of each HLS segment. Key frames fall every two
// seconds, so every segment starts with one.
const SegmentSeconds = 4

// EncodeOptions describe one encoding of a source into HLS.
type EncodeOptions struct {
	FFmpeg   string
	Source   string
	Probe    Probe
	Rungs    []Rung
	Threads  int
	OutDir   string
	Progress func(fraction float64)
}

// MasterPlaylist is the name of the playlist that lists the qualities.
const MasterPlaylist = "master.m3u8"

// VariantPlaylist is the name of the playlist of one quality.
func VariantPlaylist(name string) string { return "index_" + name + ".m3u8" }

// EncodeHLS encodes the source into every quality at once, decoding it only once.
// It writes the master playlist, a playlist per quality, and MPEG-TS segments, all
// in OutDir, so that the playlists refer to their files by plain names.
func EncodeHLS(ctx context.Context, opts EncodeOptions) error {
	if err := os.MkdirAll(opts.OutDir, 0o755); err != nil {
		return err
	}
	count := len(opts.Rungs)
	var filter strings.Builder
	fmt.Fprintf(&filter, "[0:v]split=%d", count)
	for i := range opts.Rungs {
		fmt.Fprintf(&filter, "[s%d]", i)
	}
	for i, rung := range opts.Rungs {
		fmt.Fprintf(&filter, ";[s%d]scale=%d:%d:flags=bicubic,setsar=1[v%d]", i, rung.Width, rung.Height, i)
	}

	args := []string{"-hide_banner", "-nostdin", "-y", "-i", opts.Source, "-filter_complex", filter.String()}
	for i := range opts.Rungs {
		args = append(args, "-map", fmt.Sprintf("[v%d]", i))
		if opts.Probe.HasAudio {
			args = append(args, "-map", "0:a:0")
		}
	}
	args = append(args,
		"-c:v", "libx264", "-preset", "veryfast", "-profile:v", "high", "-pix_fmt", "yuv420p",
		// A key frame every two seconds, and none in between, so segments line up across qualities.
		"-force_key_frames", "expr:gte(t,n_forced*2)", "-sc_threshold", "0",
	)
	for i, rung := range opts.Rungs {
		args = append(args,
			fmt.Sprintf("-b:v:%d", i), fmt.Sprintf("%dk", rung.VideoKbps),
			fmt.Sprintf("-maxrate:v:%d", i), fmt.Sprintf("%dk", rung.VideoKbps*107/100),
			fmt.Sprintf("-bufsize:v:%d", i), fmt.Sprintf("%dk", rung.VideoKbps*3/2),
		)
	}
	streams := make([]string, 0, count)
	if opts.Probe.HasAudio {
		args = append(args, "-c:a", "aac", "-ac", "2", "-ar", "48000")
		for i, rung := range opts.Rungs {
			args = append(args, fmt.Sprintf("-b:a:%d", i), fmt.Sprintf("%dk", rung.AudioKbps))
			streams = append(streams, fmt.Sprintf("v:%d,a:%d,name:%s", i, i, rung.Name))
		}
	} else {
		for i, rung := range opts.Rungs {
			streams = append(streams, fmt.Sprintf("v:%d,name:%s", i, rung.Name))
		}
	}
	if opts.Threads > 0 {
		args = append(args, "-threads", fmt.Sprint(opts.Threads))
	}
	args = append(args,
		"-f", "hls",
		"-hls_time", fmt.Sprint(SegmentSeconds),
		"-hls_playlist_type", "vod",
		"-hls_flags", "independent_segments",
		"-hls_segment_type", "mpegts",
		"-hls_segment_filename", opts.OutDir+"/%v_%05d.ts",
		"-master_pl_name", MasterPlaylist,
		"-var_stream_map", strings.Join(streams, " "),
		"-progress", "pipe:1", "-nostats",
		opts.OutDir+"/index_%v.m3u8",
	)
	return run(ctx, opts.FFmpeg, args, progressReader(opts.Probe.Duration, opts.Progress))
}
