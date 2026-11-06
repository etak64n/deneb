package config

import (
	"runtime"
	"strings"
	"testing"
	"time"
)

func loadFrom(values map[string]string) (Config, error) {
	return load(func(name string) string { return values[name] })
}

func TestDefaults(t *testing.T) {
	cfg, err := loadFrom(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != ":8080" || cfg.DataDir != "/data" || cfg.Storage != StorageLocal {
		t.Errorf("listen %q, data %q, storage %q", cfg.Listen, cfg.DataDir, cfg.Storage)
	}
	if cfg.Workers != 1 || cfg.Threads != runtime.GOMAXPROCS(0) {
		t.Errorf("workers %d, threads %d", cfg.Workers, cfg.Threads)
	}
	if cfg.MaxUploadSize != 50_000_000_000 || cfg.PlaybackTTL != 24*time.Hour {
		t.Errorf("max upload %d, playback TTL %s", cfg.MaxUploadSize, cfg.PlaybackTTL)
	}
	if cfg.TrustProxy || cfg.Captions.Enabled || cfg.Captions.Model != "base" || cfg.Captions.Language != "auto" {
		t.Errorf("trust proxy %v, captions %+v", cfg.TrustProxy, cfg.Captions)
	}
	if !cfg.S3.PathStyle || cfg.S3.Region != "auto" {
		t.Errorf("S3 %+v", cfg.S3)
	}
}

func TestValues(t *testing.T) {
	cfg, err := loadFrom(map[string]string{
		"DENEB_PUBLIC_URL":           "https://video.example.com/",
		"DENEB_TRUST_PROXY":          "true",
		"DENEB_WORKERS":              "2",
		"DENEB_THREADS":              "4",
		"DENEB_MAX_UPLOAD_GB":        "5",
		"DENEB_PLAYBACK_TTL_HOURS":   "6",
		"DENEB_CAPTIONS":             "yes",
		"DENEB_CAPTIONS_MODEL":       "large-v3-turbo",
		"DENEB_STORAGE":              "S3",
		"DENEB_S3_ENDPOINT":          "https://s3.example.com",
		"DENEB_S3_BUCKET":            "videos",
		"DENEB_S3_ACCESS_KEY_ID":     "id",
		"DENEB_S3_SECRET_ACCESS_KEY": "secret",
		"DENEB_S3_PATH_STYLE":        "false",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PublicURL != "https://video.example.com" {
		t.Errorf("public URL %q keeps its trailing slash", cfg.PublicURL)
	}
	if !cfg.TrustProxy || cfg.Workers != 2 || cfg.Threads != 4 || cfg.MaxUploadSize != 5_000_000_000 || cfg.PlaybackTTL != 6*time.Hour {
		t.Errorf("config %+v", cfg)
	}
	if !cfg.Captions.Enabled || cfg.Captions.Model != "large-v3-turbo" {
		t.Errorf("captions %+v", cfg.Captions)
	}
	if cfg.Storage != StorageS3 || cfg.S3.PathStyle || cfg.S3.Bucket != "videos" {
		t.Errorf("storage %q, S3 %+v", cfg.Storage, cfg.S3)
	}
}

func TestProblemsTogether(t *testing.T) {
	_, err := loadFrom(map[string]string{
		"DENEB_PUBLIC_URL":     "video.example.com",
		"DENEB_WORKERS":        "0",
		"DENEB_CAPTIONS":       "maybe",
		"DENEB_CAPTIONS_MODEL": "huge",
		"DENEB_STORAGE":        "s3",
	})
	if err == nil {
		t.Fatal("no error")
	}
	for _, want := range []string{
		"DENEB_PUBLIC_URL", "DENEB_WORKERS", "DENEB_CAPTIONS must", "DENEB_CAPTIONS_MODEL",
		"DENEB_S3_BUCKET", "DENEB_S3_ACCESS_KEY_ID", "DENEB_S3_SECRET_ACCESS_KEY", "DENEB_S3_REGION",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not name %s: %v", want, err)
		}
	}
}

func TestAmazonS3NeedsRegion(t *testing.T) {
	values := map[string]string{
		"DENEB_STORAGE":              "s3",
		"DENEB_S3_BUCKET":            "videos",
		"DENEB_S3_ACCESS_KEY_ID":     "id",
		"DENEB_S3_SECRET_ACCESS_KEY": "secret",
	}
	if _, err := loadFrom(values); err == nil || !strings.Contains(err.Error(), "DENEB_S3_REGION") {
		t.Errorf("error %v", err)
	}
	values["DENEB_S3_REGION"] = "us-east-1"
	if _, err := loadFrom(values); err != nil {
		t.Error(err)
	}
}

func TestUnknownStorage(t *testing.T) {
	if _, err := loadFrom(map[string]string{"DENEB_STORAGE": "ftp"}); err == nil || !strings.Contains(err.Error(), "DENEB_STORAGE") {
		t.Errorf("error %v", err)
	}
}
