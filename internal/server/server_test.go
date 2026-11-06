package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/etak64n/deneb/internal/app"
	"github.com/etak64n/deneb/internal/config"
	"github.com/etak64n/deneb/internal/storage"
	"github.com/etak64n/deneb/internal/store"
)

type harness struct {
	t      *testing.T
	app    *app.App
	files  storage.Storage
	dir    string
	url    string
	client *http.Client
}

// newHarness runs a deneb server on a free port, with a stand-in for the built dashboard.
func newHarness(t *testing.T, change func(*config.Config)) *harness {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	dir := t.TempDir()
	db, err := store.Open(ctx, filepath.Join(dir, "deneb.db"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := storage.NewLocal(filepath.Join(dir, "media"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		DataDir: dir, Storage: config.StorageLocal, Workers: 1, Threads: 1, MaxUploadSize: 1 << 30, PlaybackTTL: 24 * time.Hour,
		Captions: config.CaptionsConfig{Model: "base", Language: "auto"}, FFmpegPath: "ffmpeg", FFprobePath: "ffprobe",
	}
	if change != nil {
		change(&cfg)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	a, err := app.New(ctx, cfg, db, files, log)
	if err != nil {
		t.Fatal(err)
	}
	ui := fstest.MapFS{
		"index.html":    {Data: []byte("<!doctype html><title>dashboard</title>")},
		"embed.html":    {Data: []byte("<!doctype html><title>embed</title>")},
		"assets/app.js": {Data: []byte("console.log(1)")},
	}
	srv, err := New(a, ui, log)
	if err != nil {
		t.Fatal(err)
	}
	go srv.RunUploads(ctx)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		ts.Close()
		cancel()
		db.Close()
	})
	jar, _ := cookiejar.New(nil)
	return &harness{t: t, app: a, files: files, dir: dir, url: ts.URL, client: &http.Client{Jar: jar}}
}

type response struct {
	status int
	header http.Header
	body   []byte
}

func (r response) json(t *testing.T, value any) {
	t.Helper()
	if err := json.Unmarshal(r.body, value); err != nil {
		t.Fatalf("decode %s: %v", r.body, err)
	}
}

// do sends a request. headers holds pairs of names and values.
func (h *harness) do(method, path string, body any, headers ...string) response {
	h.t.Helper()
	var reader io.Reader
	switch value := body.(type) {
	case nil:
	case []byte:
		reader = bytes.NewReader(value)
	case string:
		reader = strings.NewReader(value)
	default:
		encoded, _ := json.Marshal(value)
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, h.url+path, reader)
	if err != nil {
		h.t.Fatal(err)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		request.Header.Set(headers[i], headers[i+1])
	}
	result, err := h.client.Do(request)
	if err != nil {
		h.t.Fatal(err)
	}
	defer result.Body.Close()
	data, _ := io.ReadAll(result.Body)
	return response{result.StatusCode, result.Header, data}
}

// dashboard sends a request as the dashboard does, with its header.
func (h *harness) dashboard(method, path string, body any) response {
	h.t.Helper()
	return h.do(method, path, body, dashboardHeader, "test")
}

func (h *harness) signIn() {
	h.t.Helper()
	if r := h.dashboard("POST", "/auth/setup", map[string]string{"email": "admin@example.com", "password": "correct horse battery"}); r.status != http.StatusCreated {
		h.t.Fatalf("setup: %d %s", r.status, r.body)
	}
}

// apiKey creates a key of the API and returns its secret.
func (h *harness) apiKey() string {
	h.t.Helper()
	r := h.dashboard("POST", "/v1/api-keys", map[string]string{"name": "test"})
	if r.status != http.StatusCreated {
		h.t.Fatalf("create a key: %d %s", r.status, r.body)
	}
	var created struct {
		APIKey struct {
			Secret string `json:"secret"`
		} `json:"api_key"`
	}
	r.json(h.t, &created)
	return created.APIKey.Secret
}

type createdVideo struct {
	Video  app.VideoView  `json:"video"`
	Upload app.UploadView `json:"upload"`
}

func TestDashboard(t *testing.T) {
	h := newHarness(t, nil)
	for _, path := range []string{"/", "/videos/vid_a", "/api-keys"} {
		r := h.do("GET", path, nil)
		if r.status != http.StatusOK || !strings.Contains(string(r.body), "<title>dashboard</title>") || r.header.Get("X-Frame-Options") != "DENY" {
			t.Errorf("GET %s: %d %v %s", path, r.status, r.header, r.body)
		}
	}
	if r := h.do("GET", "/assets/app.js", nil); r.status != http.StatusOK || !strings.Contains(r.header.Get("Cache-Control"), "immutable") {
		t.Errorf("asset: %d %v", r.status, r.header)
	}
	// An asset of another version is missing, not the dashboard.
	if r := h.do("GET", "/assets/app-old.js", nil); r.status != http.StatusNotFound || strings.Contains(string(r.body), "<title>dashboard</title>") {
		t.Errorf("a missing asset: %d %s", r.status, r.body)
	}
	if r := h.do("POST", "/", nil); r.status != http.StatusMethodNotAllowed {
		t.Errorf("POST /: %d", r.status)
	}
	if r := h.do("GET", "/v1/nothing", nil); r.status != http.StatusNotFound || !strings.Contains(string(r.body), `"error"`) {
		t.Errorf("unknown endpoint: %d %s", r.status, r.body)
	}
	if r := h.do("GET", "/healthz", nil); r.status != http.StatusOK {
		t.Errorf("healthz: %d", r.status)
	}
}

func TestSetupAndSession(t *testing.T) {
	h := newHarness(t, nil)
	var state struct {
		SetupRequired bool `json:"setup_required"`
		User          *struct {
			Email string `json:"email"`
		} `json:"user"`
	}
	h.do("GET", "/auth/state", nil).json(t, &state)
	if !state.SetupRequired || state.User != nil {
		t.Fatalf("state %+v", state)
	}
	credentials := map[string]string{"email": "admin@example.com", "password": "correct horse battery"}
	// A page on another site cannot send the header of the dashboard.
	if r := h.do("POST", "/auth/setup", credentials); r.status != http.StatusForbidden {
		t.Errorf("setup without the header: %d", r.status)
	}
	r := h.dashboard("POST", "/auth/setup", credentials)
	if r.status != http.StatusCreated {
		t.Fatalf("setup: %d %s", r.status, r.body)
	}
	cookie := r.header.Get("Set-Cookie")
	if !strings.Contains(cookie, sessionCookie+"=") || !strings.Contains(cookie, "HttpOnly") || !strings.Contains(cookie, "SameSite=Lax") || strings.Contains(cookie, "Secure") {
		t.Errorf("cookie %q", cookie)
	}
	h.do("GET", "/auth/state", nil).json(t, &state)
	if state.SetupRequired || state.User == nil || state.User.Email != "admin@example.com" {
		t.Errorf("state after setup %+v", state)
	}
	if r := h.dashboard("POST", "/auth/setup", credentials); r.status != http.StatusBadRequest {
		t.Errorf("second setup: %d", r.status)
	}
	if r := h.dashboard("POST", "/auth/logout", nil); r.status != http.StatusNoContent {
		t.Errorf("logout: %d", r.status)
	}
	state.User = nil
	h.do("GET", "/auth/state", nil).json(t, &state)
	if state.User != nil {
		t.Errorf("still signed in: %+v", state)
	}
	if r := h.do("GET", "/v1/videos", nil); r.status != http.StatusUnauthorized {
		t.Errorf("videos after logout: %d", r.status)
	}
	if r := h.dashboard("POST", "/auth/login", map[string]string{"email": "admin@example.com", "password": "wrong password!"}); r.status != http.StatusUnauthorized {
		t.Errorf("wrong password: %d", r.status)
	}
	if r := h.dashboard("POST", "/auth/login", credentials); r.status != http.StatusOK {
		t.Errorf("login: %d %s", r.status, r.body)
	}
	if r := h.do("GET", "/v1/videos", nil); r.status != http.StatusOK {
		t.Errorf("videos after login: %d", r.status)
	}
}

func TestSessionChangesNeedTheDashboardHeader(t *testing.T) {
	h := newHarness(t, nil)
	h.signIn()
	if r := h.do("POST", "/v1/api-keys", map[string]string{"name": "x"}); r.status != http.StatusForbidden {
		t.Errorf("a change without the header: %d", r.status)
	}
	if r := h.do("GET", "/v1/api-keys", nil); r.status != http.StatusOK {
		t.Errorf("a read without the header: %d", r.status)
	}
}

func TestAPIKeys(t *testing.T) {
	h := newHarness(t, nil)
	h.signIn()
	secret := h.apiKey()
	bearer := func(method, path string, body any, key string) int {
		return h.do(method, path, body, "Authorization", "Bearer "+key).status
	}
	h.client.Jar, _ = cookiejar.New(nil) // Only the key from here on.
	if status := bearer("GET", "/v1/videos", nil, secret); status != http.StatusOK {
		t.Errorf("videos with the key: %d", status)
	}
	if status := bearer("POST", "/v1/videos", map[string]string{}, secret); status != http.StatusCreated {
		t.Errorf("a key needs no dashboard header: %d", status)
	}
	if status := bearer("POST", "/v1/api-keys", map[string]string{"name": "x"}, secret); status != http.StatusForbidden {
		t.Errorf("a key made a key: %d", status)
	}
	if status := bearer("GET", "/v1/videos", nil, secret+"x"); status != http.StatusUnauthorized {
		t.Errorf("a wrong key: %d", status)
	}
	if r := h.do("GET", "/v1/videos", nil); r.status != http.StatusUnauthorized {
		t.Errorf("no key: %d", r.status)
	}

	var listed struct {
		APIKeys []map[string]any `json:"api_keys"`
	}
	h.dashboard("POST", "/auth/login", map[string]string{"email": "admin@example.com", "password": "correct horse battery"})
	h.do("GET", "/v1/api-keys", nil).json(t, &listed)
	if len(listed.APIKeys) != 1 || listed.APIKeys[0]["secret"] != nil || listed.APIKeys[0]["last_used_at"] == nil {
		t.Fatalf("keys %+v", listed)
	}
	if r := h.dashboard("DELETE", "/v1/api-keys/"+listed.APIKeys[0]["id"].(string), nil); r.status != http.StatusNoContent {
		t.Errorf("revoke: %d", r.status)
	}
	if status := bearer("GET", "/v1/videos", nil, secret); status != http.StatusUnauthorized {
		t.Errorf("a revoked key: %d", status)
	}
}

func TestLoginLimit(t *testing.T) {
	h := newHarness(t, nil)
	wrong := map[string]string{"email": "admin@example.com", "password": "wrong password!"}
	for i := range 10 {
		if r := h.dashboard("POST", "/auth/login", wrong); r.status != http.StatusUnauthorized {
			t.Fatalf("attempt %d: %d", i+1, r.status)
		}
	}
	if r := h.dashboard("POST", "/auth/login", wrong); r.status != http.StatusTooManyRequests {
		t.Errorf("attempt 11: %d", r.status)
	}
}

func TestLoginLimitIgnoresForgedAddresses(t *testing.T) {
	h := newHarness(t, nil)
	wrong := map[string]string{"email": "admin@example.com", "password": "wrong password!"}
	for i := range 11 {
		r := h.do("POST", "/auth/login", wrong, dashboardHeader, "test", "X-Forwarded-For", "203.0.113."+strconv.Itoa(i))
		if i == 10 && r.status != http.StatusTooManyRequests {
			t.Errorf("a forged X-Forwarded-For got around the limit: %d", r.status)
		}
	}
}

func TestVideos(t *testing.T) {
	h := newHarness(t, nil)
	h.signIn()
	var created createdVideo
	r := h.dashboard("POST", "/v1/videos", map[string]string{"title": "  Talk  "})
	if r.status != http.StatusCreated {
		t.Fatalf("create: %d %s", r.status, r.body)
	}
	r.json(t, &created)
	if created.Video.Title != "Talk" || created.Video.Status != store.VideoWaiting || created.Video.Playback != nil {
		t.Errorf("video %+v", created.Video)
	}
	if created.Upload.URL != h.url+"/v1/uploads/" || created.Upload.Metadata["video_id"] != created.Video.ID || created.Upload.Metadata["token"] == "" {
		t.Errorf("upload %+v", created.Upload)
	}
	if r := h.dashboard("POST", "/v1/videos", map[string]string{"title": strings.Repeat("a", 301)}); r.status != http.StatusBadRequest {
		t.Errorf("a long title: %d", r.status)
	}
	if r := h.dashboard("POST", "/v1/videos", map[string]string{"name": "x"}); r.status != http.StatusBadRequest {
		t.Errorf("an unknown field: %d", r.status)
	}
	// The list orders videos by the millisecond of their creation.
	time.Sleep(5 * time.Millisecond)
	h.dashboard("POST", "/v1/videos", map[string]string{"title": "Second"})

	var page struct {
		Videos     []app.VideoView `json:"videos"`
		NextCursor string          `json:"next_cursor"`
	}
	h.do("GET", "/v1/videos?limit=1", nil).json(t, &page)
	if len(page.Videos) != 1 || page.Videos[0].Title != "Second" || page.NextCursor == "" {
		t.Fatalf("first page %+v", page)
	}
	cursor := page.NextCursor
	page = struct {
		Videos     []app.VideoView `json:"videos"`
		NextCursor string          `json:"next_cursor"`
	}{}
	h.do("GET", "/v1/videos?limit=1&cursor="+cursor, nil).json(t, &page)
	if len(page.Videos) != 1 || page.Videos[0].Title != "Talk" || page.NextCursor != "" {
		t.Errorf("second page %+v", page)
	}
	for _, query := range []string{"limit=0", "limit=101", "limit=x", "cursor=nope"} {
		if r := h.do("GET", "/v1/videos?"+query, nil); r.status != http.StatusBadRequest {
			t.Errorf("%s: %d", query, r.status)
		}
	}

	path := "/v1/videos/" + created.Video.ID
	var renamed struct {
		Video app.VideoView `json:"video"`
	}
	h.dashboard("PATCH", path, map[string]string{"title": "Keynote"}).json(t, &renamed)
	if renamed.Video.Title != "Keynote" {
		t.Errorf("renamed %+v", renamed.Video)
	}
	if r := h.dashboard("DELETE", path, nil); r.status != http.StatusNoContent {
		t.Errorf("delete: %d", r.status)
	}
	for _, method := range []string{"GET", "PATCH", "DELETE"} {
		if r := h.dashboard(method, path, map[string]string{}); r.status != http.StatusNotFound {
			t.Errorf("%s a deleted video: %d", method, r.status)
		}
	}
}

// tusCreate starts an upload of size bytes with the metadata, and returns the response.
func (h *harness) tusCreate(size int, metadata map[string]string, headers ...string) response {
	h.t.Helper()
	pairs := []string{}
	for name, value := range metadata {
		pairs = append(pairs, name+" "+base64.StdEncoding.EncodeToString([]byte(value)))
	}
	all := append([]string{"Tus-Resumable", "1.0.0", "Upload-Length", strconv.Itoa(size), "Upload-Metadata", strings.Join(pairs, ",")}, headers...)
	return h.do("POST", "/v1/uploads/", nil, all...)
}

func TestUpload(t *testing.T) {
	h := newHarness(t, nil)
	h.signIn()
	var created createdVideo
	h.dashboard("POST", "/v1/videos", map[string]string{"title": "Clip"}).json(t, &created)
	id, token := created.Video.ID, created.Upload.Metadata["token"]
	data := bytes.Repeat([]byte("frame "), 10_000)

	if r := h.tusCreate(len(data), map[string]string{"video_id": id}); r.status != http.StatusUnauthorized {
		t.Errorf("an upload without a token: %d %s", r.status, r.body)
	}
	if r := h.tusCreate(len(data), map[string]string{"video_id": id, "token": token + "x"}); r.status != http.StatusUnauthorized {
		t.Errorf("an upload with a wrong token: %d", r.status)
	}
	r := h.tusCreate(len(data), map[string]string{"video_id": id, "token": token, "filename": "clip.mp4"})
	if r.status != http.StatusCreated {
		t.Fatalf("create the upload: %d %s", r.status, r.body)
	}
	location := r.header.Get("Location")
	if !strings.HasPrefix(location, h.url+"/v1/uploads/") {
		t.Fatalf("location %q", location)
	}
	patch := h.do("PATCH", strings.TrimPrefix(location, h.url), data,
		"Tus-Resumable", "1.0.0", "Upload-Offset", "0", "Content-Type", "application/offset+octet-stream")
	if patch.status != http.StatusNoContent {
		t.Fatalf("send the file: %d %s", patch.status, patch.body)
	}

	deadline := time.Now().Add(10 * time.Second)
	for {
		video, err := h.app.Store().Video(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if video.Status == store.VideoQueued {
			if video.SourceSize != int64(len(data)) {
				t.Errorf("source size %d", video.SourceSize)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the video stayed %s", video.Status)
		}
		time.Sleep(20 * time.Millisecond)
	}
	stored, err := os.ReadFile(filepath.Join(h.dir, "media", "videos", id, "source"))
	if err != nil || !bytes.Equal(stored, data) {
		t.Errorf("stored source: %d bytes, %v", len(stored), err)
	}
	if r := h.tusCreate(len(data), map[string]string{"video_id": id, "token": token}); r.status != http.StatusConflict {
		t.Errorf("a second upload of the video: %d", r.status)
	}
}

func TestUploadURLs(t *testing.T) {
	start := func(h *harness, headers ...string) string {
		h.t.Helper()
		_, upload, err := h.app.CreateVideo(context.Background(), "", "")
		if err != nil {
			h.t.Fatal(err)
		}
		r := h.tusCreate(10, upload.Metadata, headers...)
		if r.status != http.StatusCreated {
			h.t.Fatalf("create the upload: %d %s", r.status, r.body)
		}
		return r.header.Get("Location")
	}
	forged := []string{"X-Forwarded-Host", "attacker.example", "X-Forwarded-Proto", "https", "Forwarded", "host=attacker.example;proto=https"}

	public := newHarness(t, func(cfg *config.Config) { cfg.PublicURL = "https://video.example.com" })
	if location := start(public, forged...); !strings.HasPrefix(location, "https://video.example.com/v1/uploads/") {
		t.Errorf("with DENEB_PUBLIC_URL: %q", location)
	}
	direct := newHarness(t, nil)
	if location := start(direct, forged...); !strings.HasPrefix(location, direct.url+"/v1/uploads/") {
		t.Errorf("without a trusted proxy: %q", location)
	}
	proxied := newHarness(t, func(cfg *config.Config) { cfg.TrustProxy = true })
	if location := start(proxied, "X-Forwarded-Host", "video.example.net", "X-Forwarded-Proto", "https"); !strings.HasPrefix(location, "https://video.example.net/v1/uploads/") {
		t.Errorf("behind a trusted proxy: %q", location)
	}
}

func TestPlayback(t *testing.T) {
	h := newHarness(t, nil)
	h.signIn()
	ctx := context.Background()
	var created createdVideo
	h.dashboard("POST", "/v1/videos", map[string]string{"title": "Clip"}).json(t, &created)
	id := created.Video.ID
	h.app.Store().QueueVideo(ctx, id, 1, time.Now())
	h.app.Store().ReadyVideo(ctx, id, 9, 1280, 720, []store.Rendition{{Name: "720p", Width: 1280, Height: 720, Bitrate: 2_928_000}}, false, time.Now())
	for key, content := range map[string]string{
		"hls/master.m3u8":   "#EXTM3U\nindex_720p.m3u8\n",
		"hls/720p_00000.ts": "segment",
		"poster.jpg":        "jpeg",
	} {
		local := filepath.Join(t.TempDir(), "file")
		os.WriteFile(local, []byte(content), 0o644)
		if err := h.files.PutFile(ctx, "videos/"+id+"/"+key, local); err != nil {
			t.Fatal(err)
		}
	}

	var got struct {
		Video app.VideoView `json:"video"`
	}
	h.do("GET", "/v1/videos/"+id, nil).json(t, &got)
	playback := got.Video.Playback
	if playback == nil {
		t.Fatal("a ready video has no playback URLs")
	}
	h.client.Jar, _ = cookiejar.New(nil) // Viewers have no session.
	master := h.do("GET", strings.TrimPrefix(playback.HLSURL, h.url), nil)
	if master.status != http.StatusOK || string(master.body) != "#EXTM3U\nindex_720p.m3u8\n" ||
		master.header.Get("Content-Type") != "application/vnd.apple.mpegurl" || master.header.Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("master playlist: %d %v %q", master.status, master.header, master.body)
	}
	root := strings.TrimSuffix(strings.TrimPrefix(playback.HLSURL, h.url), "hls/master.m3u8")
	if r := h.do("GET", root+"hls/720p_00000.ts", nil, "Range", "bytes=0-2"); r.status != http.StatusPartialContent || string(r.body) != "seg" {
		t.Errorf("a range of a segment: %d %q", r.status, r.body)
	}
	if r := h.do("GET", root+"poster.jpg", nil); r.status != http.StatusOK || r.header.Get("Content-Type") != "image/jpeg" ||
		r.header.Get("Cache-Control") != "public, max-age=86400" {
		t.Errorf("poster: %d %v", r.status, r.header)
	}
	// Caches keep files, never errors.
	if r := h.do("GET", root+"hls/720p_00009.ts", nil); r.status != http.StatusNotFound || r.header.Get("Cache-Control") != "no-store" {
		t.Errorf("a missing segment: %d %v", r.status, r.header)
	}
	for _, path := range []string{root + "source", root + "hls/missing.ts", "/play/" + id + "/123.abc/hls/master.m3u8", "/play/vid_other/" + strings.Split(root, "/")[3] + "/poster.jpg"} {
		if r := h.do("GET", path, nil); r.status != http.StatusNotFound && r.status != http.StatusForbidden {
			t.Errorf("GET %s: %d", path, r.status)
		}
	}
	if r := h.do("GET", "/play/"+id+"/123.abc/hls/master.m3u8", nil); r.status != http.StatusForbidden {
		t.Errorf("a forged token: %d", r.status)
	}
	if r := h.do("OPTIONS", root+"hls/master.m3u8", nil); r.status != http.StatusNoContent || r.header.Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("preflight: %d %v", r.status, r.header)
	}
	var info struct {
		Title    string `json:"title"`
		Captions bool   `json:"captions"`
	}
	h.do("GET", root+"info.json", nil).json(t, &info)
	if info.Title != "Clip" || info.Captions {
		t.Errorf("info %+v", info)
	}

	embed := h.do("GET", strings.TrimPrefix(playback.EmbedURL, h.url), nil)
	if embed.status != http.StatusOK || !strings.Contains(string(embed.body), "<title>embed</title>") || embed.header.Get("X-Frame-Options") != "" {
		t.Errorf("embed: %d %v %s", embed.status, embed.header, embed.body)
	}
	// The page itself says that the video is not available.
	if r := h.do("GET", "/embed/"+id+"?token=123.abc", nil); r.status != http.StatusForbidden || !strings.Contains(string(r.body), "<title>embed</title>") {
		t.Errorf("embed with a forged token: %d %s", r.status, r.body)
	}
}

func TestWebhooks(t *testing.T) {
	h := newHarness(t, nil)
	h.signIn()
	if r := h.dashboard("POST", "/v1/webhooks", map[string]string{"url": "ftp://example.com"}); r.status != http.StatusBadRequest {
		t.Errorf("an ftp URL: %d", r.status)
	}
	var created struct {
		Webhook struct {
			ID     string `json:"id"`
			URL    string `json:"url"`
			Secret string `json:"secret"`
		} `json:"webhook"`
	}
	h.dashboard("POST", "/v1/webhooks", map[string]string{"url": "https://example.com/hooks/deneb"}).json(t, &created)
	if created.Webhook.ID == "" || !strings.HasPrefix(created.Webhook.Secret, "whsec_") {
		t.Fatalf("webhook %+v", created.Webhook)
	}
	var listed struct {
		Webhooks []map[string]any `json:"webhooks"`
	}
	h.do("GET", "/v1/webhooks", nil).json(t, &listed)
	if len(listed.Webhooks) != 1 || listed.Webhooks[0]["secret"] != nil || listed.Webhooks[0]["url"] != "https://example.com/hooks/deneb" {
		t.Errorf("webhooks %+v", listed)
	}
	if r := h.do("GET", "/v1/webhooks/"+created.Webhook.ID+"/deliveries", nil); r.status != http.StatusOK || !strings.Contains(string(r.body), `"deliveries":[]`) {
		t.Errorf("deliveries: %d %s", r.status, r.body)
	}
	if r := h.dashboard("DELETE", "/v1/webhooks/"+created.Webhook.ID, nil); r.status != http.StatusNoContent {
		t.Errorf("delete: %d", r.status)
	}
	if r := h.dashboard("DELETE", "/v1/webhooks/"+created.Webhook.ID, nil); r.status != http.StatusNotFound {
		t.Errorf("delete again: %d", r.status)
	}
}
