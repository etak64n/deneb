// Package app is what deneb does, apart from HTTP: it creates videos, takes their
// uploads, encodes them in the background, signs the URLs of their playback, and
// tells webhooks about them.
package app

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/etak64n/deneb/internal/config"
	"github.com/etak64n/deneb/internal/storage"
	"github.com/etak64n/deneb/internal/store"
)

// UploadTTL is how long the upload URL of a new video stays valid.
const UploadTTL = 24 * time.Hour

// App holds the state of a running deneb.
type App struct {
	cfg     config.Config
	store   *store.Store
	storage storage.Storage
	secret  []byte
	log     *slog.Logger
	now     func() time.Time

	wakeMu sync.Mutex
	wakers []chan struct{}

	// cancels stops the job that works on a video, by the ID of the video.
	cancelsMu sync.Mutex
	cancels   map[string]context.CancelFunc
}

// New returns the app. It takes the secret that signs tokens from the
// configuration, or from the database, where it creates one on the first start.
func New(ctx context.Context, cfg config.Config, db *store.Store, files storage.Storage, log *slog.Logger) (*App, error) {
	secret := cfg.Secret
	if secret == "" {
		stored, found, err := db.Setting(ctx, "signing_secret")
		if err != nil {
			return nil, err
		}
		if !found {
			stored = randomString(32)
			if err := db.SetSetting(ctx, "signing_secret", stored); err != nil {
				return nil, err
			}
		}
		secret = stored
	}
	return &App{
		cfg: cfg, store: db, storage: files, secret: []byte(secret), log: log,
		now:     func() time.Time { return time.Now().UTC() },
		cancels: map[string]context.CancelFunc{},
	}, nil
}

// Config returns the configuration of the app.
func (a *App) Config() config.Config { return a.cfg }

// Store returns the database.
func (a *App) Store() *store.Store { return a.store }

// Storage returns where the files of the videos live.
func (a *App) Storage() storage.Storage { return a.storage }

// Now returns the current time in UTC.
func (a *App) Now() time.Time { return a.now() }

// NewID returns a random ID with a prefix that tells its kind, such as "vid_" for a video.
func NewID(prefix string) string {
	var raw [16]byte
	rand.Read(raw[:])
	return prefix + "_" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw[:]))
}

// randomString returns a random string of URL-safe characters that holds n random bytes.
func randomString(n int) string {
	raw := make([]byte, n)
	rand.Read(raw)
	return base64.RawURLEncoding.EncodeToString(raw)
}

// HashSecret returns the SHA-256 of a secret, which the database keeps instead of the secret.
func HashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// sign returns a token that proves deneb allowed the purpose for the video until exp.
// Its form is "<unix seconds>.<MAC>", which is safe in a path and in a query.
func (a *App) sign(purpose, videoID string, exp time.Time) string {
	expires := strconv.FormatInt(exp.Unix(), 10)
	return expires + "." + a.mac(purpose, videoID, expires)
}

func (a *App) mac(purpose, videoID, expires string) string {
	h := hmac.New(sha256.New, a.secret)
	fmt.Fprintf(h, "%s\n%s\n%s", purpose, videoID, expires)
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

// verify checks a token from sign, and that it has not expired.
func (a *App) verify(purpose, videoID, token string) bool {
	expires, mac, found := strings.Cut(token, ".")
	if !found {
		return false
	}
	seconds, err := strconv.ParseInt(expires, 10, 64)
	if err != nil || a.now().Unix() > seconds {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(mac), []byte(a.mac(purpose, videoID, expires))) == 1
}

// VerifyUploadToken checks the token of an upload URL.
func (a *App) VerifyUploadToken(videoID, token string) bool {
	return a.verify("upload", videoID, token)
}

// VerifyPlaybackToken checks the token in a playback URL.
func (a *App) VerifyPlaybackToken(videoID, token string) bool {
	return a.verify("playback", videoID, token)
}

// Storage keys of the files of a video.
func videoPrefix(id string) string { return "videos/" + id + "/" }
func sourceKey(id string) string   { return videoPrefix(id) + "source" }

// PlaybackKey turns the path of a playback URL into the key of a file, or "" when
// the path names no file that deneb serves.
func PlaybackKey(videoID, path string) string {
	if strings.Contains(path, "..") || strings.Contains(path, "\\") {
		return ""
	}
	switch {
	case path == "poster.jpg", path == "storyboard.jpg", path == "storyboard.vtt", path == "captions.vtt":
		return videoPrefix(videoID) + path
	case strings.HasPrefix(path, "hls/") && !strings.Contains(path[len("hls/"):], "/"):
		name := path[len("hls/"):]
		if strings.HasSuffix(name, ".m3u8") || strings.HasSuffix(name, ".ts") {
			return videoPrefix(videoID) + path
		}
	}
	return ""
}

// VideoView is the form of a video in the API and in webhook events.
type VideoView struct {
	ID         string            `json:"id"`
	Title      string            `json:"title"`
	Status     string            `json:"status"`
	Error      string            `json:"error,omitempty"`
	Progress   float64           `json:"progress"`
	Duration   float64           `json:"duration"`
	Width      int               `json:"width"`
	Height     int               `json:"height"`
	SourceSize int64             `json:"source_size"`
	Renditions []store.Rendition `json:"renditions"`
	Captions   CaptionsView      `json:"captions"`
	CreatedAt  time.Time         `json:"created_at"`
	ReadyAt    *time.Time        `json:"ready_at"`
	Playback   *PlaybackView     `json:"playback,omitempty"`
}

// CaptionsView is the state of the captions of a video.
type CaptionsView struct {
	Status   string `json:"status"`
	Language string `json:"language,omitempty"`
}

// PlaybackView holds the signed URLs that play a ready video until ExpiresAt.
type PlaybackView struct {
	HLSURL        string    `json:"hls_url"`
	PosterURL     string    `json:"poster_url"`
	StoryboardURL string    `json:"storyboard_url"`
	CaptionsURL   string    `json:"captions_url,omitempty"`
	EmbedURL      string    `json:"embed_url"`
	ExpiresAt     time.Time `json:"expires_at"`
}

// View returns the API form of a video. URLs start with base, such as
// https://video.example.com, or are paths when base is empty.
func (a *App) View(video store.Video, base string) VideoView {
	view := VideoView{
		ID: video.ID, Title: video.Title, Status: video.Status, Error: video.Error, Progress: video.Progress,
		Duration: video.Duration, Width: video.Width, Height: video.Height, SourceSize: video.SourceSize,
		Renditions: video.Renditions, CreatedAt: video.CreatedAt, ReadyAt: video.ReadyAt,
		Captions: CaptionsView{Status: video.CaptionsStatus, Language: video.CaptionsLanguage},
	}
	if view.Renditions == nil {
		view.Renditions = []store.Rendition{}
	}
	if video.Status == store.VideoReady {
		// Rounding down to the hour keeps the URLs the same for an hour, so that
		// players and caches reuse them. They stay valid for at least TTL minus an hour.
		exp := a.now().Add(a.cfg.PlaybackTTL).Truncate(time.Hour)
		token := a.sign("playback", video.ID, exp)
		root := fmt.Sprintf("%s/play/%s/%s/", base, video.ID, token)
		view.Playback = &PlaybackView{
			HLSURL:        root + "hls/master.m3u8",
			PosterURL:     root + "poster.jpg",
			StoryboardURL: root + "storyboard.vtt",
			EmbedURL:      fmt.Sprintf("%s/embed/%s?token=%s", base, video.ID, token),
			ExpiresAt:     exp,
		}
		if video.CaptionsStatus == store.CaptionsReady {
			view.Playback.CaptionsURL = root + "captions.vtt"
		}
	}
	return view
}

// UploadView tells a client where to send the file of a new video.
type UploadView struct {
	// URL is the tus endpoint. Clients send the file to it with the metadata below.
	URL       string            `json:"url"`
	Metadata  map[string]string `json:"metadata"`
	ExpiresAt time.Time         `json:"expires_at"`
}

// CreateVideo creates a video, and returns where to upload its file.
func (a *App) CreateVideo(ctx context.Context, title, base string) (store.Video, UploadView, error) {
	video, err := a.store.CreateVideo(ctx, NewID("vid"), title, a.now())
	if err != nil {
		return store.Video{}, UploadView{}, err
	}
	exp := a.now().Add(UploadTTL).Truncate(time.Second)
	return video, UploadView{
		URL:       base + "/v1/uploads/",
		Metadata:  map[string]string{"video_id": video.ID, "token": a.sign("upload", video.ID, exp)},
		ExpiresAt: exp,
	}, nil
}

// CompleteUpload stores the uploaded file of a video and queues its encoding.
func (a *App) CompleteUpload(ctx context.Context, videoID, localPath string, size int64) error {
	if err := a.storage.MoveFile(ctx, sourceKey(videoID), localPath); err != nil {
		return fmt.Errorf("store the source: %w", err)
	}
	if err := a.store.QueueVideo(ctx, videoID, size, a.now()); err != nil {
		return err
	}
	a.Wake()
	return nil
}

// DeleteVideo removes a video and its files, and stops the job that works on it.
func (a *App) DeleteVideo(ctx context.Context, id string) error {
	if err := a.store.DeleteVideo(ctx, id); err != nil {
		return err
	}
	a.cancelsMu.Lock()
	if cancel, found := a.cancels[id]; found {
		cancel()
	}
	a.cancelsMu.Unlock()
	if err := a.storage.DeletePrefix(ctx, videoPrefix(id)); err != nil {
		// The video is gone from the database; files left behind only take space.
		a.log.Error("delete the files of a video", "video", id, "error", err)
	}
	return nil
}

// emit queues an event about a video for every webhook.
func (a *App) emit(ctx context.Context, event, videoID string) {
	video, err := a.store.Video(ctx, videoID)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			a.log.Error("read a video for an event", "video", videoID, "error", err)
		}
		return
	}
	payload, err := json.Marshal(map[string]any{
		"type":       event,
		"created_at": a.now(),
		"data":       map[string]any{"video": a.View(video, a.cfg.PublicURL)},
	})
	if err != nil {
		a.log.Error("encode an event", "event", event, "error", err)
		return
	}
	if err := a.store.QueueEvent(ctx, event, string(payload), func() string { return NewID("whd") }, a.now()); err != nil {
		a.log.Error("queue an event", "event", event, "error", err)
		return
	}
	a.Wake()
}

// workDir is where the encoders of a video work.
func (a *App) workDir(videoID string) string {
	return filepath.Join(a.cfg.DataDir, "work", videoID)
}
