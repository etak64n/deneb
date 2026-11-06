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
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/etak64n/deneb/internal/config"
	"github.com/etak64n/deneb/internal/storage"
	"github.com/etak64n/deneb/internal/store"
)

func newApp(t *testing.T) *App {
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
		Captions: config.CaptionsConfig{Model: "base", Language: "auto"},
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
	if want := time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC); !first.ExpiresAt.Equal(want) {
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
	if _, _, err := a.Setup(ctx, "admin@example.com", "short"); err == nil {
		t.Error("a short password was accepted")
	}
	if _, _, err := a.Setup(ctx, "not an email", "correct horse battery"); err == nil {
		t.Error("an invalid email was accepted")
	}
	user, token, err := a.Setup(ctx, " Admin@Example.com ", "correct horse battery")
	if err != nil || user.Email != "admin@example.com" || token == "" {
		t.Fatalf("setup: %+v, %q, %v", user, token, err)
	}
	if _, _, err := a.Setup(ctx, "other@example.com", "correct horse battery"); err == nil {
		t.Error("a second setup succeeded")
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

func TestDeleteVideoStopsItsJob(t *testing.T) {
	ctx := context.Background()
	a := newApp(t)
	a.store.CreateVideo(ctx, "vid_a", "", a.now())
	stopped := false
	a.cancels["vid_a"] = func() { stopped = true }
	if err := a.DeleteVideo(ctx, "vid_a"); err != nil {
		t.Fatal(err)
	}
	if !stopped {
		t.Error("the job of the deleted video kept running")
	}
	if err := a.DeleteVideo(ctx, "vid_a"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("second delete: %v", err)
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
