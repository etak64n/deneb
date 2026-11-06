package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
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

// The limits of the programs of an encoding. ffprobe and the pictures take seconds.
// The stream takes as long as the video needs, so ffmpeg stops only when it reports no
// progress for stallTimeout, as when it hangs on a broken file.
const (
	probeTimeout   = 2 * time.Minute
	pictureTimeout = 10 * time.Minute
	stallTimeout   = 10 * time.Minute
)

// ErrSourceGone says that neither the upload of a video nor its stored source is
// there anymore, so no attempt can encode it.
var ErrSourceGone = errors.New("the file of the video is gone; create the video again and upload its file")

// transcode turns the source of a video into its HLS stream, poster, and storyboard.
// runJob decides what a failure means for the video.
func (a *App) transcode(ctx context.Context, id string) error {
	video, err := a.store.Video(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil // The video was deleted while it waited.
		}
		return err
	}
	if video.Status == store.VideoReady {
		// The encoding finished before, and only the record of its job did not.
		return nil
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
		if stored, err := a.storage.Exists(ctx, sourceKey(id)); err == nil && !stored {
			return permanentError{ErrSourceGone}
		}
		stored, cleanup, err := a.storage.LocalFile(ctx, sourceKey(id), filepath.Join(work, "source"))
		if err != nil {
			return fmt.Errorf("read the source: %w", err)
		}
		defer cleanup()
		source = stored
	}
	probeCtx, cancelProbe := context.WithTimeout(ctx, probeTimeout)
	probe, err := media.ProbeFile(probeCtx, a.cfg.FFprobePath, source)
	cancelProbe()
	if err != nil {
		if errors.Is(err, media.ErrNotAVideo) {
			// Another attempt cannot fix the file. Any other failure of ffprobe, such
			// as one that could not start, may pass.
			return permanentError{err}
		}
		return err
	}
	rungs := media.Ladder(probe.Width, probe.Height)
	if err := a.roomForEncoding(probe, rungs); err != nil {
		return err
	}

	encodeCtx, watch := watchProgress(ctx, stallTimeout, "ffmpeg")
	defer watch.stop()
	// The database learns the progress at most every two seconds.
	var lastReport time.Time
	progress := func(fraction float64) {
		watch.advance(fraction)
		if time.Since(lastReport) < 2*time.Second {
			return
		}
		lastReport = time.Now()
		if err := a.store.SetVideoProgress(ctx, id, fraction*0.95, a.now()); err != nil && ctx.Err() == nil {
			a.log.Warn("record the progress of a video", "video", id, "error", err)
		}
	}
	hlsDir := filepath.Join(work, "hls")
	if err := media.EncodeHLS(encodeCtx, media.EncodeOptions{
		FFmpeg: a.cfg.FFmpegPath, Source: source, Probe: probe, Rungs: rungs, Threads: a.cfg.Threads, OutDir: hlsDir, Progress: progress,
	}); err != nil {
		return watch.explain(err)
	}
	pictureCtx, cancelPictures := context.WithTimeout(ctx, pictureTimeout)
	defer cancelPictures()
	if err := media.Poster(pictureCtx, a.cfg.FFmpegPath, source, probe, filepath.Join(work, "poster.jpg")); err != nil {
		return fmt.Errorf("make the poster: %w", err)
	}
	smallest := filepath.Join(hlsDir, media.VariantPlaylist(rungs[len(rungs)-1].Name))
	if err := media.Storyboard(pictureCtx, a.cfg.FFmpegPath, smallest, probe.Duration, work); err != nil {
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
	files := map[string]string{}
	for _, entry := range entries {
		files[videoPrefix(id)+"hls/"+entry.Name()] = filepath.Join(hlsDir, entry.Name())
	}
	for _, name := range []string{"poster.jpg", "storyboard.jpg", "storyboard.vtt"} {
		files[videoPrefix(id)+name] = filepath.Join(work, name)
	}
	if err := a.moveAll(ctx, files); err != nil {
		return err
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
	// The stream is stored. Its record and its event go through even when deneb begins
	// to stop now, so that the next start does not encode the video again.
	done := context.WithoutCancel(ctx)
	captions := a.cfg.Captions.Enabled && probe.HasAudio
	if err := a.store.ReadyVideo(done, id, probe.Duration, probe.Width, probe.Height, renditions, captions, a.now()); err != nil {
		return err
	}
	if captions {
		a.Wake()
	}
	a.log.Info("encoded a video", "video", id, "duration", probe.Duration, "renditions", len(rungs))
	a.emit(done, EventVideoReady, id)
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
	// whisper.cpp reports no progress that deneb reads, so a transcription stops only
	// after ten times the length of the video, and at least an hour, which only one
	// that hangs takes.
	limit := max(time.Hour, time.Duration(video.Duration*10*float64(time.Second)))
	ctx, cancel := context.WithTimeoutCause(ctx, limit,
		fmt.Errorf("whisper.cpp ran for %.0f minutes, ten times the length of the video or at least an hour, so deneb stopped it", limit.Minutes()))
	defer cancel()
	err = a.transcribe(ctx, id)
	if errors.Is(err, context.DeadlineExceeded) {
		if cause := context.Cause(ctx); cause != nil {
			return cause
		}
	}
	return err
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
	// The captions are stored. Their record and event go through even when deneb
	// begins to stop now.
	done := context.WithoutCancel(ctx)
	if err := a.store.SetCaptions(done, id, store.CaptionsReady, language, a.now()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		return err
	}
	a.log.Info("wrote the captions of a video", "video", id, "language", language)
	a.emit(done, EventVideoCaptionsReady, id)
	return nil
}

// storeParallel is how many files moveAll stores at once. A bucket takes many at once
// far faster than one after another, and a rename on the volume costs nothing.
const storeParallel = 8

// moveAll moves the files of the work folder into the storage, under their keys: a
// rename each on the volume, and an upload each to a bucket, storeParallel at a time.
// It returns the first error, and stops the rest when one fails.
func (a *App) moveAll(ctx context.Context, files map[string]string) error {
	work, cancel := context.WithCancel(ctx)
	defer cancel()
	type move struct{ key, local string }
	moves := make(chan move)
	failures := make(chan error, 1)
	var wg sync.WaitGroup
	for range storeParallel {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for next := range moves {
				if err := a.storage.MoveFile(work, next.key, next.local); err != nil {
					select {
					case failures <- err:
					default:
					}
					cancel()
				}
			}
		}()
	}
	for key, local := range files {
		select {
		case moves <- move{key, local}:
		case <-work.Done():
		}
	}
	close(moves)
	wg.Wait()
	select {
	case err := <-failures:
		return err
	default:
		return ctx.Err()
	}
}
