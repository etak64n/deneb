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
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
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

	// client sends the webhooks, except to the addresses that blockedAddress names.
	client *http.Client

	wakeMu sync.Mutex
	wakers []chan struct{}

	// running holds the jobs at work on videos by their ID, so that deleting a video stops them.
	runningMu sync.Mutex
	running   map[int64]runningJob

	// hashing lets few passwords be checked at once, since each check takes 19 MiB.
	hashing chan struct{}

	// setupCode is the code that creating the first administrator asks for, from the log.
	setupMu   sync.Mutex
	setupCode string
}

type runningJob struct {
	video  string
	cancel context.CancelFunc
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
	a := &App{
		cfg: cfg, store: db, storage: files, secret: []byte(secret), log: log,
		now:     func() time.Time { return time.Now().UTC() },
		client:  newWebhookClient(),
		running: map[int64]runningJob{},
		hashing: make(chan struct{}, 2),
	}
	required, err := a.SetupRequired(ctx)
	if err != nil {
		return nil, err
	}
	if required {
		a.setupCode = newSetupCode()
	}
	return a, nil
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

// PermanentToken stands in the place of the token in the permanent URLs of a video,
// which never expire. No signed token can equal it, since those hold a dot.
const PermanentToken = "permanent"

// PlaybackAllowed tells whether a playback URL with this token may serve the video:
// a signed token that has not expired, or PermanentToken while the video has permanent
// URLs. It reads the video for PermanentToken, so that taking those away ends them at once.
func (a *App) PlaybackAllowed(ctx context.Context, videoID, token string) (bool, error) {
	if token != PermanentToken {
		return a.VerifyPlaybackToken(videoID, token), nil
	}
	video, err := a.store.Video(ctx, videoID)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return video.PermanentURLs, nil
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
	ID            string            `json:"id"`
	Title         string            `json:"title"`
	PermanentURLs bool              `json:"permanent_urls"`
	Status        string            `json:"status"`
	Error         string            `json:"error,omitempty"`
	Progress      float64           `json:"progress"`
	Duration      float64           `json:"duration"`
	Width         int               `json:"width"`
	Height        int               `json:"height"`
	SourceSize    int64             `json:"source_size"`
	Renditions    []store.Rendition `json:"renditions"`
	Captions      CaptionsView      `json:"captions"`
	CreatedAt     time.Time         `json:"created_at"`
	ReadyAt       *time.Time        `json:"ready_at"`
	Playback      *PlaybackView     `json:"playback,omitempty"`
}

// CaptionsView is the state of the captions of a video.
type CaptionsView struct {
	Status   string `json:"status"`
	Language string `json:"language,omitempty"`
}

// PlaybackView holds the URLs that play a ready video: signed ones until ExpiresAt,
// or, for a video with permanent URLs, ones that never expire, with a nil ExpiresAt.
type PlaybackView struct {
	HLSURL        string     `json:"hls_url"`
	PosterURL     string     `json:"poster_url"`
	StoryboardURL string     `json:"storyboard_url"`
	CaptionsURL   string     `json:"captions_url,omitempty"`
	EmbedURL      string     `json:"embed_url"`
	ExpiresAt     *time.Time `json:"expires_at"`
}

// View returns the API form of a video. URLs start with base, such as
// https://video.example.com, or are paths when base is empty.
func (a *App) View(video store.Video, base string) VideoView {
	view := VideoView{
		ID: video.ID, Title: video.Title, PermanentURLs: video.PermanentURLs, Status: video.Status, Error: video.Error, Progress: video.Progress,
		Duration: video.Duration, Width: video.Width, Height: video.Height, SourceSize: video.SourceSize,
		Renditions: video.Renditions, CreatedAt: video.CreatedAt, ReadyAt: video.ReadyAt,
		Captions: CaptionsView{Status: video.CaptionsStatus, Language: video.CaptionsLanguage},
	}
	if view.Renditions == nil {
		view.Renditions = []store.Rendition{}
	}
	if video.Status == store.VideoReady {
		// Permanent URLs never change, and their embed URL has no token.
		token, embed, expires := PermanentToken, fmt.Sprintf("%s/embed/%s", base, video.ID), (*time.Time)(nil)
		if !video.PermanentURLs {
			// Rounding up to the hour keeps the URLs the same for an hour, so that players
			// and caches reuse them. They stay valid for at least TTL, at most an hour more.
			exp := a.now().Add(a.cfg.PlaybackTTL)
			if hour := exp.Truncate(time.Hour); !hour.Equal(exp) {
				exp = hour.Add(time.Hour)
			}
			token = a.sign("playback", video.ID, exp)
			embed += "?token=" + token
			expires = &exp
		}
		root := fmt.Sprintf("%s/play/%s/%s/", base, video.ID, token)
		view.Playback = &PlaybackView{
			HLSURL:        root + "hls/master.m3u8",
			PosterURL:     root + "poster.jpg",
			StoryboardURL: root + "storyboard.vtt",
			EmbedURL:      embed,
			ExpiresAt:     expires,
		}
		if video.CaptionsStatus == store.CaptionsReady {
			view.Playback.CaptionsURL = root + "captions.vtt"
		}
	}
	return view
}

// UploadView tells a client where to send the file of a new video, in one of two ways.
// Both carry the upload token of the video, which works until ExpiresAt.
type UploadView struct {
	// URL takes the whole file in one PUT, as the signed URLs of buckets do.
	URL string `json:"url"`
	// Tus creates an upload in the tus protocol, which resumes after a lost connection.
	Tus       TusView   `json:"tus"`
	ExpiresAt time.Time `json:"expires_at"`
}

// TusView is where a tus client creates the upload of a video, and with what metadata.
type TusView struct {
	Endpoint string            `json:"endpoint"`
	Metadata map[string]string `json:"metadata"`
}

// CreateVideo creates a video, with permanent URLs or not, and returns where to upload its file.
func (a *App) CreateVideo(ctx context.Context, title, base string, permanentURLs bool) (store.Video, UploadView, error) {
	video, err := a.store.CreateVideo(ctx, NewID("vid"), title, a.now())
	if err != nil {
		return store.Video{}, UploadView{}, err
	}
	if permanentURLs {
		if err := a.store.SetVideoPermanentURLs(ctx, video.ID, true, a.now()); err != nil {
			return store.Video{}, UploadView{}, err
		}
		video.PermanentURLs = true
	}
	exp := a.now().Add(UploadTTL).Truncate(time.Second)
	token := a.sign("upload", video.ID, exp)
	return video, UploadView{
		URL: base + "/v1/uploads/" + video.ID + "?token=" + url.QueryEscape(token),
		Tus: TusView{
			Endpoint: base + "/v1/uploads/",
			Metadata: map[string]string{"video_id": video.ID, "token": token},
		},
		ExpiresAt: exp,
	}, nil
}

// UploadPath is where the file of a video arrives. tusd writes it there, under the ID
// of the video, and keeps what it knows of the upload next to it, in UploadPath + ".info".
func (a *App) UploadPath(videoID string) string {
	return filepath.Join(a.cfg.DataDir, "uploads", videoID)
}

// RemoveUpload removes the file of a video as it arrived, and the record of tusd.
func (a *App) RemoveUpload(videoID string) {
	path := a.UploadPath(videoID)
	for _, name := range []string{path, path + ".info"} {
		if err := os.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
			a.log.Warn("remove an upload", "path", name, "error", err)
		}
	}
}

// UploadFinished queues the encoding of a video whose file arrived, and returns the
// video as queued. It reads the video before it wakes the workers, since a worker may
// take the video at once, and the answer to an upload shows it queued. The file stays
// where it arrived until the encoder has stored it, so that a stop loses nothing. It
// returns store.ErrNotFound when the video is gone or no longer waits for its file.
func (a *App) UploadFinished(ctx context.Context, videoID string, size int64) (store.Video, error) {
	if err := a.store.QueueVideo(ctx, videoID, size, a.now()); err != nil {
		return store.Video{}, err
	}
	video, err := a.store.Video(ctx, videoID)
	a.Wake()
	return video, err
}

// recoverUploads queues the videos whose file arrived but which deneb did not queue,
// as when it stopped at that moment.
func (a *App) recoverUploads(ctx context.Context) {
	records, _ := filepath.Glob(filepath.Join(a.cfg.DataDir, "uploads", "*.info"))
	for _, record := range records {
		data, err := os.ReadFile(record)
		if err != nil {
			continue
		}
		// The fields of tusd's FileInfo that tell whether the whole file arrived.
		var info struct {
			ID             string
			Size           int64
			Offset         int64
			SizeIsDeferred bool
		}
		if json.Unmarshal(data, &info) != nil || info.SizeIsDeferred || info.Offset != info.Size {
			continue
		}
		video, err := a.store.Video(ctx, info.ID)
		if err != nil || video.Status != store.VideoWaiting {
			continue
		}
		if _, err := a.UploadFinished(ctx, info.ID, info.Size); err != nil {
			a.log.Error("queue a video whose file arrived", "video", info.ID, "error", err)
			continue
		}
		a.log.Info("queued a video whose file arrived before deneb stopped", "video", info.ID)
	}
}

// DeleteVideo removes a video, stops the jobs that work on it, and queues the removal of its files.
func (a *App) DeleteVideo(ctx context.Context, id string) error {
	if err := a.store.DeleteVideo(ctx, id, a.now()); err != nil {
		return err
	}
	a.runningMu.Lock()
	for _, job := range a.running {
		if job.video == id {
			job.cancel()
		}
	}
	a.runningMu.Unlock()
	a.Wake()
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
