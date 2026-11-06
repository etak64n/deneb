package media

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// ErrNoAudio is returned for captions of a video without sound.
var ErrNoAudio = errors.New("the video has no sound track")

// modelURL is where whisper.cpp publishes its models in the ggml format.
const modelURL = "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-%s.bin"

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
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf(modelURL, name), nil)
	if err != nil {
		return "", err
	}
	response, err := http.DefaultClient.Do(request)
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
	if _, err := io.Copy(file, response.Body); err != nil {
		file.Close()
		os.Remove(partial)
		return "", fmt.Errorf("download the whisper.cpp model %s: %w", name, err)
	}
	if err := file.Close(); err != nil {
		os.Remove(partial)
		return "", err
	}
	return target, os.Rename(partial, target)
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
