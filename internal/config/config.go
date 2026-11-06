// Package config reads the settings of deneb from environment variables. Every
// setting has a default that works for one container with a volume at /data, so
// `docker run` needs none of them.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Storage kinds: the volume of the container, or a bucket of an S3-compatible service.
const (
	StorageLocal = "local"
	StorageS3    = "s3"
)

// Config holds every setting of a deneb server.
type Config struct {
	// Listen is the address of the HTTP server, such as ":8080".
	Listen string
	// DataDir holds the database, the uploads in progress, the work of the
	// encoders, the Whisper models, and the videos when Storage is "local".
	DataDir string
	// PublicURL is the URL that viewers and API clients reach deneb at, such as
	// https://video.example.com. Without it, deneb builds URLs from each request.
	PublicURL string
	// Secret signs the tokens of uploads and playback. Without it, deneb creates
	// one on the first start and keeps it in the database.
	Secret string
	// TrustProxy makes deneb read the address, scheme, and host of each client from
	// the X-Forwarded-For, X-Forwarded-Proto, and X-Forwarded-Host headers, which a
	// reverse proxy in front of deneb sets. Without a proxy, clients could forge them.
	TrustProxy bool

	Storage string
	S3      S3Config

	// Workers is the number of videos that deneb encodes at once.
	Workers int
	// Threads is the number of CPU threads that each encoder may use, 0 for all of them.
	Threads int
	// MaxUploadSize is the largest upload in bytes.
	MaxUploadSize int64
	// PlaybackTTL is how long the signed playback URLs that the API returns stay valid.
	PlaybackTTL time.Duration

	Captions CaptionsConfig

	FFmpegPath  string
	FFprobePath string
}

// S3Config names a bucket of any S3-compatible service: Amazon S3, Cloudflare R2,
// IDrive e2, MinIO, RustFS, and others.
type S3Config struct {
	Endpoint        string
	Region          string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	// PathStyle puts the bucket in the path rather than in the host name, as most
	// services other than Amazon S3 need.
	PathStyle bool
}

// CaptionsConfig turns on subtitles that whisper.cpp writes on this machine.
type CaptionsConfig struct {
	Enabled bool
	// Model is the whisper.cpp model, such as "base" or "small". Larger models are
	// more accurate and slower. deneb downloads the model on first use.
	Model string
	// Language is the spoken language, such as "ja", or "auto" to detect it.
	Language string
	// WhisperPath is the whisper.cpp command line program.
	WhisperPath string
}

// Load reads the configuration from the environment.
func Load() (Config, error) {
	return load(os.Getenv)
}

func load(getenv func(string) string) (Config, error) {
	env := func(name, fallback string) string {
		if value := strings.TrimSpace(getenv(name)); value != "" {
			return value
		}
		return fallback
	}
	var problems []string
	number := func(name string, fallback, min int) int {
		raw := env(name, "")
		if raw == "" {
			return fallback
		}
		value, err := strconv.Atoi(raw)
		if err != nil || value < min {
			problems = append(problems, fmt.Sprintf("%s must be a whole number of at least %d", name, min))
			return fallback
		}
		return value
	}
	flag := func(name string, fallback bool) bool {
		switch strings.ToLower(env(name, "")) {
		case "":
			return fallback
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
		problems = append(problems, fmt.Sprintf("%s must be true or false", name))
		return fallback
	}

	cfg := Config{
		Listen:        env("DENEB_LISTEN", ":8080"),
		DataDir:       env("DENEB_DATA_DIR", "/data"),
		PublicURL:     strings.TrimRight(env("DENEB_PUBLIC_URL", ""), "/"),
		Secret:        env("DENEB_SECRET", ""),
		TrustProxy:    flag("DENEB_TRUST_PROXY", false),
		Storage:       strings.ToLower(env("DENEB_STORAGE", StorageLocal)),
		Workers:       number("DENEB_WORKERS", 1, 1),
		Threads:       number("DENEB_THREADS", 0, 0),
		MaxUploadSize: int64(number("DENEB_MAX_UPLOAD_GB", 50, 1)) * 1_000_000_000,
		PlaybackTTL:   time.Duration(number("DENEB_PLAYBACK_TTL_HOURS", 24, 1)) * time.Hour,
		S3: S3Config{
			Endpoint:        env("DENEB_S3_ENDPOINT", ""),
			Region:          env("DENEB_S3_REGION", "auto"),
			Bucket:          env("DENEB_S3_BUCKET", ""),
			AccessKeyID:     env("DENEB_S3_ACCESS_KEY_ID", ""),
			SecretAccessKey: env("DENEB_S3_SECRET_ACCESS_KEY", ""),
			PathStyle:       flag("DENEB_S3_PATH_STYLE", true),
		},
		Captions: CaptionsConfig{
			Enabled:     flag("DENEB_CAPTIONS", false),
			Model:       env("DENEB_CAPTIONS_MODEL", "base"),
			Language:    env("DENEB_CAPTIONS_LANGUAGE", "auto"),
			WhisperPath: env("DENEB_WHISPER_PATH", "whisper-cli"),
		},
		FFmpegPath:  env("DENEB_FFMPEG_PATH", "ffmpeg"),
		FFprobePath: env("DENEB_FFPROBE_PATH", "ffprobe"),
	}

	if cfg.PublicURL != "" {
		parsed, err := url.Parse(cfg.PublicURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			problems = append(problems, "DENEB_PUBLIC_URL must be an http or https URL, such as https://video.example.com")
		}
	}
	switch cfg.Storage {
	case StorageLocal:
	case StorageS3:
		for _, required := range []struct{ name, value string }{
			{"DENEB_S3_BUCKET", cfg.S3.Bucket},
			{"DENEB_S3_ACCESS_KEY_ID", cfg.S3.AccessKeyID},
			{"DENEB_S3_SECRET_ACCESS_KEY", cfg.S3.SecretAccessKey},
		} {
			if required.value == "" {
				problems = append(problems, fmt.Sprintf("%s is required when DENEB_STORAGE is s3", required.name))
			}
		}
		// "auto" is the region of Cloudflare R2. Amazon S3, which needs no endpoint, needs a real region.
		if cfg.S3.Endpoint == "" && cfg.S3.Region == "auto" {
			problems = append(problems, "DENEB_S3_REGION is required for Amazon S3, such as us-east-1")
		}
	default:
		problems = append(problems, `DENEB_STORAGE must be "local" or "s3"`)
	}
	if !validModel(cfg.Captions.Model) {
		problems = append(problems, "DENEB_CAPTIONS_MODEL must be one of tiny, base, small, medium, large-v3, large-v3-turbo")
	}
	if cfg.Threads == 0 {
		// GOMAXPROCS follows the CPU limit of the container, unlike the count of CPUs of the host.
		cfg.Threads = runtime.GOMAXPROCS(0)
	}
	if len(problems) > 0 {
		return Config{}, errors.New("invalid configuration: " + strings.Join(problems, "; "))
	}
	return cfg, nil
}

// The whisper.cpp models that deneb downloads, by the names of their files.
var models = []string{"tiny", "base", "small", "medium", "large-v3", "large-v3-turbo"}

func validModel(name string) bool {
	for _, model := range models {
		if name == model {
			return true
		}
	}
	return false
}
