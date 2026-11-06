package media

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ErrNoAudio is returned for captions of a video without sound.
var ErrNoAudio = errors.New("the video has no sound track")

// modelURL is where whisper.cpp publishes its models in the ggml format.
const modelURL = "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-%s.bin"

// modelClient downloads the models. A server that does not answer within half a minute
// fails the download, which runs again with its job.
var modelClient = &http.Client{Transport: &http.Transport{
	Proxy:                 http.ProxyFromEnvironment,
	DialContext:           (&net.Dialer{Timeout: 30 * time.Second}).DialContext,
	TLSHandshakeTimeout:   30 * time.Second,
	ResponseHeaderTimeout: 30 * time.Second,
}}

// modelIdle is how long a download may receive nothing before it stops. A model takes
// minutes to arrive, so the download as a whole has no limit.
const modelIdle = 2 * time.Minute

// EnsureModel returns the path of a whisper.cpp model in dir, and downloads it
// first if this machine does not have it yet.
func EnsureModel(ctx context.Context, dir, name string) (string, error) {
	target := filepath.Join(dir, "ggml-"+name+".bin")
	if _, err := os.Stat(target); err == nil {
		return target, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	removeStaleParts(dir, name)
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf(modelURL, name), nil)
	if err != nil {
		return "", err
	}
	response, err := modelClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("download the whisper.cpp model %s: %w", name, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download the whisper.cpp model %s: %s", name, response.Status)
	}
	// A file of its own for each download, so that two jobs that download the same
	// model at once do not write into one file.
	file, err := os.CreateTemp(dir, "ggml-"+name+"-*.part")
	if err != nil {
		return "", err
	}
	partial := file.Name()
	idle := time.AfterFunc(modelIdle, func() {
		cancel(fmt.Errorf("the download of the whisper.cpp model %s received nothing for %s", name, modelIdle))
	})
	defer idle.Stop()
	body := readerFunc(func(p []byte) (int, error) {
		n, err := response.Body.Read(p)
		if n > 0 {
			idle.Reset(modelIdle)
		}
		return n, err
	})
	if _, err := io.Copy(file, body); err != nil {
		file.Close()
		os.Remove(partial)
		if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, context.Canceled) {
			return "", cause
		}
		return "", fmt.Errorf("download the whisper.cpp model %s: %w", name, err)
	}
	if err := file.Close(); err != nil {
		os.Remove(partial)
		return "", err
	}
	return target, os.Rename(partial, target)
}

// readerFunc turns a function into an io.Reader.
type readerFunc func(p []byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }

// removeStaleParts removes what downloads of a model left when deneb stopped in their
// middle. A part that changed within the hour may be a download of another job.
func removeStaleParts(dir, name string) {
	parts, _ := filepath.Glob(filepath.Join(dir, "ggml-"+name+"-*.part"))
	for _, part := range parts {
		if info, err := os.Stat(part); err == nil && time.Since(info.ModTime()) > time.Hour {
			os.Remove(part)
		}
	}
}

// CaptionsOptions describe one transcription.
type CaptionsOptions struct {
	FFmpeg   string
	Whisper  string
	Model    string
	Source   string
	HasAudio bool
	// Language is a code such as "ja", or "auto" to let whisper.cpp detect it.
	Language string
	Threads  int
	Dir      string
}

// Captions transcribes the speech of a video into WebVTT subtitles with whisper.cpp.
// It returns the path of the subtitles and the language that they are in.
func Captions(ctx context.Context, opts CaptionsOptions) (string, string, error) {
	if !opts.HasAudio {
		return "", "", ErrNoAudio
	}
	if err := os.MkdirAll(opts.Dir, 0o755); err != nil {
		return "", "", err
	}
	// whisper.cpp reads 16 kHz mono WAV.
	audio := filepath.Join(opts.Dir, "audio.wav")
	if err := run(ctx, opts.FFmpeg, []string{
		"-hide_banner", "-nostdin", "-y", "-i", opts.Source,
		"-vn", "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", audio,
	}, nil); err != nil {
		return "", "", err
	}
	defer os.Remove(audio)
	base := filepath.Join(opts.Dir, "captions")
	args := []string{"-m", opts.Model, "-f", audio, "-l", opts.Language, "-ovtt", "-oj", "-of", base, "-np"}
	if opts.Threads > 0 {
		args = append(args, "-t", fmt.Sprint(opts.Threads))
	}
	if err := run(ctx, opts.Whisper, args, nil); err != nil {
		return "", "", err
	}
	language := opts.Language
	if raw, err := os.ReadFile(base + ".json"); err == nil {
		var output struct {
			Result struct {
				Language string `json:"language"`
			} `json:"result"`
		}
		if json.Unmarshal(raw, &output) == nil && output.Result.Language != "" {
			language = output.Result.Language
		}
	}
	if language == "auto" {
		language = ""
	}
	return base + ".vtt", strings.ToLower(language), nil
}
