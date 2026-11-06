package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/etak64n/deneb/internal/config"
	"github.com/etak64n/deneb/internal/storage"
	"github.com/etak64n/deneb/internal/store"
)

// newApp returns an app whose webhooks may reach the receivers of the tests, which
// listen on the loopback.
func newApp(t *testing.T) *App {
	t.Helper()
	return newAppWith(t, nil)
}

// newAppWith returns an app with the configuration of newApp, which adjust changes.
func newAppWith(t *testing.T, adjust func(*config.Config)) *App {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(ctx, filepath.Join(dir, "deneb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	files, err := storage.NewLocal(filepath.Join(dir, "media"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		DataDir: dir, Workers: 1, Threads: 1, PlaybackTTL: 24 * time.Hour,
		WebhookAllowNetworks: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128")},
		Captions:             config.CaptionsConfig{Model: "base", Language: "auto"},
	}
	if adjust != nil {
		adjust(&cfg)
	}
	a, err := New(ctx, cfg, db, files, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestTokens(t *testing.T) {
	a := newApp(t)
	exp := a.now().Add(time.Hour)
	token := a.sign("playback", "vid_a", exp)
	if !a.VerifyPlaybackToken("vid_a", token) {
		t.Fatal("a valid token failed")
	}
	if a.VerifyUploadToken("vid_a", token) {
		t.Error("a playback token works for an upload")
	}
	if a.VerifyPlaybackToken("vid_b", token) {
		t.Error("a token works for another video")
	}
	_, mac, _ := strings.Cut(token, ".")
	if a.VerifyPlaybackToken("vid_a", fmt.Sprint(exp.Add(time.Hour).Unix())+"."+mac) {
		t.Error("a token with a later expiry works")
	}
	for _, bad := range []string{"", ".", "abc", "123.", "x.y", token + "x"} {
		if a.VerifyPlaybackToken("vid_a", bad) {
			t.Errorf("token %q works", bad)
		}
	}
	a.now = func() time.Time { return exp.Add(time.Second) }
	if a.VerifyPlaybackToken("vid_a", token) {
		t.Error("an expired token works")
	}
}

func TestSecretPersists(t *testing.T) {
	ctx := context.Background()
	a := newApp(t)
	token := a.sign("playback", "vid_a", a.now().Add(time.Hour))
	again, err := New(ctx, a.cfg, a.store, a.storage, a.log)
	if err != nil {
		t.Fatal(err)
	}
	if !again.VerifyPlaybackToken("vid_a", token) {
		t.Error("a token from before a restart failed")
	}
	cfg := a.cfg
	cfg.Secret = "set-in-the-environment"
	configured, _ := New(ctx, cfg, a.store, a.storage, a.log)
	if configured.VerifyPlaybackToken("vid_a", token) {
		t.Error("DENEB_SECRET did not replace the stored secret")
	}
}

func TestPlaybackKey(t *testing.T) {
	cases := map[string]string{
		"hls/master.m3u8":       "videos/vid_a/hls/master.m3u8",
		"hls/index_720p.m3u8":   "videos/vid_a/hls/index_720p.m3u8",
		"hls/720p_00001.ts":     "videos/vid_a/hls/720p_00001.ts",
		"poster.jpg":            "videos/vid_a/poster.jpg",
		"storyboard.jpg":        "videos/vid_a/storyboard.jpg",
		"storyboard.vtt":        "videos/vid_a/storyboard.vtt",
		"captions.vtt":          "videos/vid_a/captions.vtt",
		"source":                "",
		"hls/../source":         "",
		"hls/x/720p_00001.ts":   "",
		"hls/clip.mp4":          "",
		"../vid_b/poster.jpg":   "",
		"hls\\720p_00001.ts":    "",
		"hls/":                  "",
		"storyboard.jpg/../x":   "",
		"videos/vid_a/source":   "",
		"hls/master.m3u8/extra": "",
	}
	for path, want := range cases {
		if got := PlaybackKey("vid_a", path); got != want {
			t.Errorf("PlaybackKey(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestViewSignsReadyVideosForAnHour(t *testing.T) {
	ctx := context.Background()
	a := newApp(t)
	at := time.Date(2026, 11, 6, 10, 15, 0, 0, time.UTC)
	a.now = func() time.Time { return at }
	video, _ := a.store.CreateVideo(ctx, "vid_a", "Talk", at)
	if view := a.View(video, "https://video.example.com"); view.Playback != nil {
		t.Errorf("a waiting video has playback URLs: %+v", view.Playback)
	}
	a.store.QueueVideo(ctx, "vid_a", 1, at)
	a.store.ReadyVideo(ctx, "vid_a", 10, 1280, 720, nil, false, at)
	video, _ = a.store.Video(ctx, "vid_a")

	first := a.View(video, "https://video.example.com").Playback
	if first == nil || !strings.HasPrefix(first.HLSURL, "https://video.example.com/play/vid_a/") || !strings.HasSuffix(first.HLSURL, "/hls/master.m3u8") {
		t.Fatalf("playback %+v", first)
	}
	if first.CaptionsURL != "" || !strings.HasPrefix(first.EmbedURL, "https://video.example.com/embed/vid_a?token=") {
		t.Errorf("playback %+v", first)
	}
	if want := time.Date(2026, 11, 7, 11, 0, 0, 0, time.UTC); first.ExpiresAt == nil || !first.ExpiresAt.Equal(want) {
		t.Errorf("expires at %v, want %v", first.ExpiresAt, want)
	}
	a.now = func() time.Time { return at.Add(40 * time.Minute) }
	if later := a.View(video, "https://video.example.com").Playback; later.HLSURL != first.HLSURL {
		t.Error("the URL changed within the hour")
	}
	a.now = func() time.Time { return at.Add(50 * time.Minute) }
	if next := a.View(video, "https://video.example.com").Playback; next.HLSURL == first.HLSURL {
		t.Error("the URL stayed the same in the next hour")
	}
}

func TestPermanentURLs(t *testing.T) {
	ctx := context.Background()
	a := newApp(t)
	video, _, err := a.CreateVideo(ctx, "Talk", "https://video.example.com", true)
	if err != nil || !video.PermanentURLs {
		t.Fatalf("create a video with permanent URLs: %+v, %v", video, err)
	}
	a.store.QueueVideo(ctx, video.ID, 1, a.now())
	a.store.ReadyVideo(ctx, video.ID, 10, 1280, 720, nil, false, a.now())
	video, _ = a.store.Video(ctx, video.ID)

	view := a.View(video, "https://video.example.com")
	want := "https://video.example.com/play/" + video.ID + "/permanent/"
	if !view.PermanentURLs || view.Playback == nil || view.Playback.HLSURL != want+"hls/master.m3u8" || view.Playback.PosterURL != want+"poster.jpg" ||
		view.Playback.EmbedURL != "https://video.example.com/embed/"+video.ID || view.Playback.ExpiresAt != nil {
		t.Fatalf("view %+v, playback %+v", view, view.Playback)
	}
	if allowed, err := a.PlaybackAllowed(ctx, video.ID, PermanentToken); !allowed || err != nil {
		t.Errorf("permanent URLs of a video that has them: %v, %v", allowed, err)
	}
	if allowed, _ := a.PlaybackAllowed(ctx, "vid_missing", PermanentToken); allowed {
		t.Error("permanent URLs of a missing video are allowed")
	}

	// Without permanent URLs, the video plays under signed URLs only, and those end at once.
	a.store.SetVideoPermanentURLs(ctx, video.ID, false, a.now())
	video, _ = a.store.Video(ctx, video.ID)
	signed := a.View(video, "https://video.example.com").Playback
	if signed == nil || signed.ExpiresAt == nil || strings.Contains(signed.HLSURL, "/permanent/") || !strings.Contains(signed.EmbedURL, "?token=") {
		t.Fatalf("playback without permanent URLs %+v", signed)
	}
	if allowed, _ := a.PlaybackAllowed(ctx, video.ID, PermanentToken); allowed {
		t.Error("permanent URLs that were taken away are allowed")
	}
	token := strings.Split(strings.TrimPrefix(signed.HLSURL, "https://video.example.com/play/"+video.ID+"/"), "/")[0]
	if allowed, _ := a.PlaybackAllowed(ctx, video.ID, token); !allowed {
		t.Error("the signed URLs are refused")
	}
}

func TestPasswords(t *testing.T) {
	hash := HashPassword("correct horse battery")
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Errorf("hash %q", hash)
	}
	if !CheckPassword("correct horse battery", hash) {
		t.Error("the right password failed")
	}
	if CheckPassword("correct horse battery!", hash) || CheckPassword("", hash) {
		t.Error("a wrong password passed")
	}
	if HashPassword("correct horse battery") == hash {
		t.Error("two hashes of a password are the same; the salt is missing")
	}
	for _, broken := range []string{"", "$argon2id$", "$bcrypt$v=19$m=1,t=1,p=1$c2FsdA$a2V5", "$argon2id$v=19$m=x$c2FsdA$a2V5"} {
		if CheckPassword("x", broken) {
			t.Errorf("hash %q passed", broken)
		}
	}
}

func TestSetupAndLogin(t *testing.T) {
	ctx := context.Background()
	a := newApp(t)
	if required, _ := a.SetupRequired(ctx); !required {
		t.Fatal("setup is not required on a new server")
	}
	code := a.SetupCode()
	if len(code) != 9 || code[4] != '-' {
		t.Fatalf("setup code %q", code)
	}
	if _, _, err := a.Setup(ctx, "admin@example.com", "correct horse battery", "AAAA-AAAA"); !errors.Is(err, ErrInvalidSetupCode) {
		t.Errorf("a wrong setup code: %v", err)
	}
	if _, _, err := a.Setup(ctx, "admin@example.com", "short", code); err == nil {
		t.Error("a short password was accepted")
	}
	if _, _, err := a.Setup(ctx, "not an email", "correct horse battery", code); err == nil {
		t.Error("an invalid email was accepted")
	}
	// The code works in lower case and without its dash.
	typed := strings.ToLower(strings.ReplaceAll(code, "-", ""))
	user, token, err := a.Setup(ctx, " Admin@Example.com ", "correct horse battery", typed)
	if err != nil || user.Email != "admin@example.com" || token == "" {
		t.Fatalf("setup: %+v, %q, %v", user, token, err)
	}
	if a.SetupCode() != "" {
		t.Error("the setup code outlived the setup")
	}
	if _, _, err := a.Setup(ctx, "other@example.com", "correct horse battery", code); !errors.Is(err, ErrSetupDone) {
		t.Errorf("a second setup: %v", err)
	}
	if session, err := a.SessionUser(ctx, token); err != nil || session.ID != user.ID {
		t.Errorf("session %+v, %v", session, err)
	}

	for _, wrong := range [][2]string{{"admin@example.com", "wrong password!"}, {"nobody@example.com", "correct horse battery"}} {
		if _, _, err := a.Login(ctx, wrong[0], wrong[1]); !errors.Is(err, ErrInvalidLogin) {
			t.Errorf("login %v: %v", wrong, err)
		}
	}
	_, second, err := a.Login(ctx, "ADMIN@example.com", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Logout(ctx, second); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SessionUser(ctx, second); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("a session works after logout: %v", err)
	}
	if _, err := a.SessionUser(ctx, token); err != nil {
		t.Errorf("logging out one browser ended another: %v", err)
	}
}

func TestAPIKeys(t *testing.T) {
	ctx := context.Background()
	a := newApp(t)
	if _, _, err := a.CreateAPIKey(ctx, "  "); err == nil {
		t.Error("a key without a name was created")
	}
	key, secret, err := a.CreateAPIKey(ctx, "CI")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(secret, "dnb_") || !strings.HasPrefix(secret, key.Prefix) || len(key.Prefix) != len("dnb_")+6 {
		t.Errorf("secret %q, prefix %q", secret, key.Prefix)
	}
	if used, err := a.UseAPIKey(ctx, secret); err != nil || used.ID != key.ID {
		t.Errorf("use: %+v, %v", used, err)
	}
	for _, wrong := range []string{"", "dnb_", secret + "x", strings.TrimPrefix(secret, "dnb_")} {
		if _, err := a.UseAPIKey(ctx, wrong); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("key %q: %v", wrong, err)
		}
	}
}

func TestDeleteVideoStopsItsJobs(t *testing.T) {
	ctx := context.Background()
	a := newApp(t)
	a.store.CreateVideo(ctx, "vid_a", "", a.now())
	stopped := map[string]bool{}
	// An encoding and the captions of the video, and a job of another video.
	a.running[1] = runningJob{video: "vid_a", cancel: func() { stopped["transcode"] = true }}
	a.running[2] = runningJob{video: "vid_a", cancel: func() { stopped["captions"] = true }}
	a.running[3] = runningJob{video: "vid_b", cancel: func() { stopped["other"] = true }}
	if err := a.DeleteVideo(ctx, "vid_a"); err != nil {
		t.Fatal(err)
	}
	if !stopped["transcode"] || !stopped["captions"] || stopped["other"] {
		t.Errorf("stopped %v", stopped)
	}
	if err := a.DeleteVideo(ctx, "vid_a"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("second delete: %v", err)
	}
}

func TestUploadFinished(t *testing.T) {
	ctx := context.Background()
	a := newApp(t)
	a.store.CreateVideo(ctx, "vid_a", "", a.now())
	if err := a.UploadFinished(ctx, "vid_a", 5); err != nil {
		t.Fatal(err)
	}
	if video, _ := a.store.Video(ctx, "vid_a"); video.Status != store.VideoQueued || video.SourceSize != 5 {
		t.Errorf("video %+v", video)
	}
	if job, _ := a.store.ClaimJob(ctx, []string{store.JobTranscode}, a.now()); job == nil || job.Ref != "vid_a" {
		t.Errorf("no encoding was queued: %+v", job)
	}
	// A second report of the same upload, and one of a deleted video, change nothing.
	if err := a.UploadFinished(ctx, "vid_a", 5); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("a second report: %v", err)
	}
	if err := a.UploadFinished(ctx, "vid_gone", 5); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("a deleted video: %v", err)
	}
}

// An upload whose last part arrived as deneb stopped is queued at the next start.
func TestRecoverUploads(t *testing.T) {
	ctx := context.Background()
	a := newApp(t)
	os.MkdirAll(filepath.Join(a.cfg.DataDir, "uploads"), 0o755)
	for id, offset := range map[string]int{"vid_done": 5, "vid_partial": 3} {
		a.store.CreateVideo(ctx, id, "", a.now())
		os.WriteFile(a.UploadPath(id), []byte("12345")[:offset], 0o644)
		os.WriteFile(a.UploadPath(id)+".info", []byte(fmt.Sprintf(`{"ID":%q,"Size":5,"Offset":%d}`, id, offset)), 0o644)
	}
	a.recoverUploads(ctx)
	if video, _ := a.store.Video(ctx, "vid_done"); video.Status != store.VideoQueued {
		t.Errorf("the finished upload is %s", video.Status)
	}
	if video, _ := a.store.Video(ctx, "vid_partial"); video.Status != store.VideoWaiting {
		t.Errorf("the unfinished upload is %s", video.Status)
	}
}

// A file that is not a video fails at once, since another attempt cannot fix it.
func TestFileThatIsNotAVideoFailsAtOnce(t *testing.T) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe is not installed")
	}
	ctx := context.Background()
	a := newApp(t)
	a.cfg.FFprobePath = "ffprobe"
	a.store.CreateVideo(ctx, "vid_a", "", a.now())
	os.MkdirAll(filepath.Join(a.cfg.DataDir, "uploads"), 0o755)
	os.WriteFile(a.UploadPath("vid_a"), []byte("these bytes are not a video"), 0o644)
	a.UploadFinished(ctx, "vid_a", 27)
	runWorkers(t, a)
	waitFor(t, "the video to fail", func() bool {
		video, _ := a.store.Video(ctx, "vid_a")
		return video.Status == store.VideoFailed
	})
	if _, err := os.Stat(a.UploadPath("vid_a")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the upload of a failed video stays: %v", err)
	}
}

// flakyStorage fails DeletePrefix a number of times before it works.
type flakyStorage struct {
	storage.Storage
	failures int
}

func (f *flakyStorage) DeletePrefix(ctx context.Context, prefix string) error {
	if f.failures > 0 {
		f.failures--
		return errors.New("the bucket did not answer")
	}
	return f.Storage.DeletePrefix(ctx, prefix)
}

func TestPurgeRetries(t *testing.T) {
	ctx := context.Background()
	a := newApp(t)
	a.storage = &flakyStorage{Storage: a.storage, failures: 1}
	a.store.CreateVideo(ctx, "vid_a", "", a.now())
	local := filepath.Join(t.TempDir(), "poster.jpg")
	os.WriteFile(local, []byte("jpeg"), 0o644)
	a.storage.PutFile(ctx, "videos/vid_a/poster.jpg", local)
	stored := filepath.Join(a.cfg.DataDir, "media", "videos", "vid_a", "poster.jpg")

	if err := a.DeleteVideo(ctx, "vid_a"); err != nil {
		t.Fatal(err)
	}
	job, _ := a.store.ClaimJob(ctx, []string{store.JobPurge}, a.now())
	if job == nil {
		t.Fatal("deleting the video queued no removal of its files")
	}
	a.purgeJob(ctx, job)
	if _, err := os.Stat(stored); err != nil {
		t.Fatalf("the file went although the storage failed: %v", err)
	}
	// The removal runs again a minute later, and the storage answers this time.
	again, _ := a.store.ClaimJob(ctx, []string{store.JobPurge}, a.now().Add(61*time.Second))
	if again == nil || again.ID != job.ID {
		t.Fatalf("no retry of the removal: %+v", again)
	}
	a.purgeJob(ctx, again)
	if _, err := os.Stat(stored); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the file of the deleted video stays: %v", err)
	}
}

// runWorkers starts the workers of the app until the test ends.
func runWorkers(t *testing.T, a *App) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		a.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
}

// waitFor checks a condition until it holds, for up to ten seconds.
func waitFor(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestWebhookDelivery(t *testing.T) {
	type request struct {
		header http.Header
		body   []byte
	}
	received := make(chan request, 1)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received <- request{r.Header.Clone(), body}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()

	ctx := context.Background()
	a := newApp(t)
	a.store.CreateVideo(ctx, "vid_a", "Talk", a.now())
	hook, err := a.CreateWebhook(ctx, receiver.URL+"/hook")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hook.Secret, "whsec_") {
		t.Errorf("secret %q", hook.Secret)
	}
	a.emit(ctx, EventVideoReady, "vid_a")
	runWorkers(t, a)

	var got request
	select {
	case got = <-received:
	case <-time.After(10 * time.Second):
		t.Fatal("the webhook received nothing")
	}
	if got.header.Get("Deneb-Event") != EventVideoReady || got.header.Get("Content-Type") != "application/json" {
		t.Errorf("headers %v", got.header)
	}
	timestamp, mac, found := strings.Cut(strings.TrimPrefix(got.header.Get("Deneb-Signature"), "t="), ",v1=")
	if !found {
		t.Fatalf("signature %q", got.header.Get("Deneb-Signature"))
	}
	expected := hmac.New(sha256.New, []byte(hook.Secret))
	expected.Write([]byte(timestamp + "." + string(got.body)))
	if mac != hex.EncodeToString(expected.Sum(nil)) {
		t.Error("the signature does not match the body")
	}
	var event struct {
		Type string `json:"type"`
		Data struct {
			Video VideoView `json:"video"`
		} `json:"data"`
	}
	if err := json.Unmarshal(got.body, &event); err != nil || event.Type != EventVideoReady || event.Data.Video.ID != "vid_a" || event.Data.Video.Title != "Talk" {
		t.Errorf("event %+v, %v: %s", event, err, got.body)
	}
	waitFor(t, "the delivery to be recorded", func() bool {
		deliveries, _ := a.store.ListDeliveries(ctx, hook.ID, 10)
		return len(deliveries) == 1 && deliveries[0].Status == store.DeliveryDelivered && deliveries[0].ResponseStatus == http.StatusNoContent
	})
}

func TestWebhookRetry(t *testing.T) {
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down for maintenance", http.StatusServiceUnavailable)
	}))
	defer receiver.Close()

	ctx := context.Background()
	a := newApp(t)
	a.store.CreateVideo(ctx, "vid_a", "", a.now())
	hook, _ := a.CreateWebhook(ctx, receiver.URL)
	a.emit(ctx, EventVideoFailed, "vid_a")
	runWorkers(t, a)

	waitFor(t, "the failed attempt to be recorded", func() bool {
		deliveries, _ := a.store.ListDeliveries(ctx, hook.ID, 10)
		return len(deliveries) == 1 && deliveries[0].Attempts == 1
	})
	deliveries, _ := a.store.ListDeliveries(ctx, hook.ID, 10)
	if d := deliveries[0]; d.Status != store.DeliveryPending || d.ResponseStatus != http.StatusServiceUnavailable || !strings.Contains(d.LastError, "503") {
		t.Errorf("delivery %+v", d)
	}
	// The next attempt waits a minute.
	if job, _ := a.store.ClaimJob(ctx, []string{store.JobWebhook}, a.now()); job != nil {
		t.Errorf("the retry is due at once: %+v", job)
	}
	var retry *store.Job
	waitFor(t, "the retry to be due after a minute", func() bool {
		retry, _ = a.store.ClaimJob(ctx, []string{store.JobWebhook}, a.now().Add(61*time.Second))
		return retry != nil
	})
	if retry.Attempts != 2 {
		t.Errorf("retry %+v", retry)
	}
}
