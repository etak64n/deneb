package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/etak64n/deneb/internal/media"
	"github.com/etak64n/deneb/internal/store"
)

// The events that deneb sends to webhooks.
const (
	EventVideoReady         = "video.ready"
	EventVideoFailed        = "video.failed"
	EventVideoCaptionsReady = "video.captions_ready"
)

// permanentError is a failure that another attempt cannot fix, such as a file that
// is not a video.
type permanentError struct{ err error }

func (e permanentError) Error() string { return e.err.Error() }
func (e permanentError) Unwrap() error { return e.err }

// transcode turns the source of a video into its HLS stream, poster, and storyboard.
// runJob decides what a failure means for the video.
func (a *App) transcode(ctx context.Context, id string) error {
	if _, err := a.store.Video(ctx, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil // The video was deleted while it waited.
		}
		return err
	}
	if err := a.store.StartVideo(ctx, id, a.now()); err != nil {
		return err
	}
	return a.encode(ctx, id)
}

func (a *App) encode(ctx context.Context, id string) error {
	work := a.workDir(id)
	os.RemoveAll(work)
	defer os.RemoveAll(work)

	// A new upload is encoded where it arrived and stored as the source after, which
	// spares a round trip to a bucket. A video encoded again reads its stored source.
	upload := a.UploadPath(id)
	source := upload
	if _, err := os.Stat(upload); err != nil {
		stored, cleanup, err := a.storage.LocalFile(ctx, sourceKey(id), filepath.Join(work, "source"))
		if err != nil {
			return fmt.Errorf("read the source: %w", err)
		}
		defer cleanup()
		source = stored
	}
	probe, err := media.ProbeFile(ctx, a.cfg.FFprobePath, source)
	if err != nil {
		return permanentError{err}
	}
	rungs := media.Ladder(probe.Width, probe.Height)

	// The database learns the progress at most every two seconds.
	var lastReport time.Time
	progress := func(fraction float64) {
		if time.Since(lastReport) < 2*time.Second {
			return
		}
		lastReport = time.Now()
		if err := a.store.SetVideoProgress(ctx, id, fraction*0.95, a.now()); err != nil && ctx.Err() == nil {
			a.log.Warn("record the progress of a video", "video", id, "error", err)
		}
	}
	hlsDir := filepath.Join(work, "hls")
	if err := media.EncodeHLS(ctx, media.EncodeOptions{
		FFmpeg: a.cfg.FFmpegPath, Source: source, Probe: probe, Rungs: rungs, Threads: a.cfg.Threads, OutDir: hlsDir, Progress: progress,
	}); err != nil {
		return err
	}
	if err := media.Poster(ctx, a.cfg.FFmpegPath, source, probe, filepath.Join(work, "poster.jpg")); err != nil {
		return fmt.Errorf("make the poster: %w", err)
	}
	smallest := filepath.Join(hlsDir, media.VariantPlaylist(rungs[len(rungs)-1].Name))
	if err := media.Storyboard(ctx, a.cfg.FFmpegPath, smallest, probe.Duration, work); err != nil {
		return fmt.Errorf("make the storyboard: %w", err)
	}

	// A video that was encoded before, such as before a restart, starts clean.
	if err := a.storage.DeletePrefix(ctx, videoPrefix(id)+"hls/"); err != nil {
		return fmt.Errorf("remove the old stream: %w", err)
	}
	entries, err := os.ReadDir(hlsDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := a.storage.PutFile(ctx, videoPrefix(id)+"hls/"+entry.Name(), filepath.Join(hlsDir, entry.Name())); err != nil {
			return err
		}
	}
	for _, name := range []string{"poster.jpg", "storyboard.jpg", "storyboard.vtt"} {
		if err := a.storage.PutFile(ctx, videoPrefix(id)+name, filepath.Join(work, name)); err != nil {
			return err
		}
	}
	if source == upload {
		if err := a.storage.MoveFile(ctx, sourceKey(id), upload); err != nil {
			return fmt.Errorf("store the source: %w", err)
		}
		a.RemoveUpload(id)
	}

	renditions := make([]store.Rendition, 0, len(rungs))
	for _, rung := range rungs {
		bitrate := rung.VideoKbps * 1000
		if probe.HasAudio {
			bitrate += rung.AudioKbps * 1000
		}
		renditions = append(renditions, store.Rendition{Name: rung.Name, Width: rung.Width, Height: rung.Height, Bitrate: bitrate})
	}
	captions := a.cfg.Captions.Enabled && probe.HasAudio
	if err := a.store.ReadyVideo(ctx, id, probe.Duration, probe.Width, probe.Height, renditions, captions, a.now()); err != nil {
		return err
	}
	if captions {
		a.Wake()
	}
	a.log.Info("encoded a video", "video", id, "duration", probe.Duration, "renditions", len(rungs))
	a.emit(ctx, EventVideoReady, id)
	return nil
}

// captions writes the subtitles of a ready video with whisper.cpp.
func (a *App) captions(ctx context.Context, id string) error {
	video, err := a.store.Video(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if video.Status != store.VideoReady {
		return nil
	}
	if err := a.store.SetCaptions(ctx, id, store.CaptionsProcessing, "", a.now()); err != nil {
		return err
	}
	return a.transcribe(ctx, id)
}

func (a *App) transcribe(ctx context.Context, id string) error {
	model, err := media.EnsureModel(ctx, filepath.Join(a.cfg.DataDir, "models"), a.cfg.Captions.Model)
	if err != nil {
		return err
	}
	work := filepath.Join(a.workDir(id), "captions")
	os.RemoveAll(work)
	defer os.RemoveAll(work)
	source, cleanup, err := a.storage.LocalFile(ctx, sourceKey(id), filepath.Join(work, "source"))
	if err != nil {
		return fmt.Errorf("read the source: %w", err)
	}
	defer cleanup()
	vtt, language, err := media.Captions(ctx, media.CaptionsOptions{
		FFmpeg: a.cfg.FFmpegPath, Whisper: a.cfg.Captions.WhisperPath, Model: model, Source: source, HasAudio: true,
		Language: a.cfg.Captions.Language, Threads: a.cfg.Threads, Dir: work,
	})
	if err != nil {
		return err
	}
	if err := a.storage.PutFile(ctx, videoPrefix(id)+"captions.vtt", vtt); err != nil {
		return err
	}
	if err := a.store.SetCaptions(ctx, id, store.CaptionsReady, language, a.now()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		return err
	}
	a.log.Info("wrote the captions of a video", "video", id, "language", language)
	a.emit(ctx, EventVideoCaptionsReady, id)
	return nil
}
