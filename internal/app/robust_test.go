package app

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/etak64n/deneb/internal/media"
	"github.com/etak64n/deneb/internal/storage"
	"github.com/etak64n/deneb/internal/store"
)

// uploadedVideo creates a video whose file arrived, and claims its encoding.
func uploadedVideo(t *testing.T, a *App, id string) *store.Job {
	t.Helper()
	ctx := context.Background()
	a.store.CreateVideo(ctx, id, "", a.now())
	os.MkdirAll(filepath.Join(a.cfg.DataDir, "uploads"), 0o755)
	os.WriteFile(a.UploadPath(id), []byte("a video, perhaps"), 0o644)
	if err := a.UploadFinished(ctx, id, 16); err != nil {
		t.Fatal(err)
	}
	job, err := a.store.ClaimJob(ctx, []string{store.JobTranscode}, a.now())
	if err != nil || job == nil {
		t.Fatalf("claim the encoding: %+v, %v", job, err)
	}
	return job
}

// A failure that may pass keeps the file of the video, which a retry encodes. Only a
// file that is not a video goes, and then a retry has nothing to encode.
func TestFailedEncodingKeepsTheFile(t *testing.T) {
	ctx := context.Background()
	a := newApp(t)
	job := uploadedVideo(t, a, "vid_a")
	a.failMedia(ctx, job, errors.New("the bucket did not answer"))
	if _, err := os.Stat(a.UploadPath("vid_a")); err != nil {
		t.Fatalf("the file of a video that failed for a reason that may pass is gone: %v", err)
	}
	if video, _ := a.store.Video(ctx, "vid_a"); video.Status != store.VideoFailed {
		t.Fatalf("status %q", video.Status)
	}

	video, err := a.RetryVideo(ctx, "vid_a")
	if err != nil || video.Status != store.VideoQueued || video.Error != "" {
		t.Fatalf("retry: %+v, %v", video, err)
	}
	if _, err := a.RetryVideo(ctx, "vid_a"); !errors.Is(err, ErrNothingToRetry) {
		t.Errorf("retry of a queued video: %v", err)
	}

	job, _ = a.store.ClaimJob(ctx, []string{store.JobTranscode}, a.now())
	a.failMedia(ctx, job, fmt.Errorf("%w: it has no video track", media.ErrNotAVideo))
	if _, err := os.Stat(a.UploadPath("vid_a")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the file that is not a video stays: %v", err)
	}
	if _, err := a.RetryVideo(ctx, "vid_a"); !errors.Is(err, ErrSourceGone) {
		t.Errorf("retry without a file: %v", err)
	}
}

func TestRetryCaptions(t *testing.T) {
	ctx := context.Background()
	a := newApp(t)
	a.store.CreateVideo(ctx, "vid_a", "", a.now())
	a.store.QueueVideo(ctx, "vid_a", 1, a.now())
	a.store.ReadyVideo(ctx, "vid_a", 10, 1280, 720, nil, true, a.now())
	if _, err := a.RetryVideo(ctx, "vid_a"); !errors.Is(err, ErrNothingToRetry) {
		t.Errorf("retry of queued captions: %v", err)
	}
	a.store.SetCaptions(ctx, "vid_a", store.CaptionsFailed, "", a.now())
	video, err := a.RetryVideo(ctx, "vid_a")
	if err != nil || video.Status != store.VideoReady || video.CaptionsStatus != store.CaptionsQueued {
		t.Errorf("retry of failed captions: %+v, %v", video, err)
	}
}

// A job that deneb stopped in the middle of, as by crashing, more often than it retries
// fails rather than run forever.
func TestJobStoppedTooOftenFails(t *testing.T) {
	ctx := context.Background()
	a := newApp(t)
	job := uploadedVideo(t, a, "vid_a")
	job.Attempts = len(mediaRetries) + 2
	a.runJob(ctx, job)
	if video, _ := a.store.Video(ctx, "vid_a"); video.Status != store.VideoFailed || video.Error != errStoppedTooOften.Error() {
		t.Errorf("video %+v", video)
	}
}

// An encoding that finished, but whose job deneb did not record, leaves the ready
// video as it is rather than encode it again.
func TestTranscodeLeavesAReadyVideo(t *testing.T) {
	ctx := context.Background()
	a := newApp(t)
	a.store.CreateVideo(ctx, "vid_a", "", a.now())
	a.store.QueueVideo(ctx, "vid_a", 1, a.now())
	a.store.ReadyVideo(ctx, "vid_a", 10, 1280, 720, nil, false, a.now())
	if err := a.transcode(ctx, "vid_a"); err != nil {
		t.Fatal(err)
	}
	if video, _ := a.store.Video(ctx, "vid_a"); video.Status != store.VideoReady {
		t.Errorf("status %q", video.Status)
	}
}

func TestProgressWatch(t *testing.T) {
	// A program whose progress grows keeps running.
	ctx, watch := watchProgress(context.Background(), 200*time.Millisecond, "ffmpeg")
	for step := 1; step <= 5; step++ {
		time.Sleep(50 * time.Millisecond)
		watch.advance(float64(step) / 10)
	}
	if ctx.Err() != nil {
		t.Fatal("the watch stopped a program that made progress")
	}
	// One whose progress stops growing stops, and the watch says why.
	watch.advance(0.5)
	time.Sleep(600 * time.Millisecond)
	if ctx.Err() == nil {
		t.Fatal("the watch let a program run that made no progress")
	}
	var stalled stalledError
	if err := watch.explain(context.Canceled); !errors.As(err, &stalled) {
		t.Errorf("explain: %v", err)
	}
	watch.stop()

	// A stop of deneb is not a stall.
	parent, cancel := context.WithCancel(context.Background())
	_, other := watchProgress(parent, time.Hour, "ffmpeg")
	cancel()
	if err := other.explain(context.Canceled); !errors.Is(err, context.Canceled) {
		t.Errorf("explain after a stop: %v", err)
	}
	other.stop()
}

func TestRoomForUpload(t *testing.T) {
	a := newApp(t)
	if _, ok := freeSpace(a.cfg.DataDir); !ok {
		t.Skip("this system does not tell the free space")
	}
	if err := a.RoomForUpload(1 << 62); err == nil || !strings.Contains(err.Error(), "free") {
		t.Errorf("a file larger than any disk: %v", err)
	}
	if err := a.RoomForUpload(1); err != nil {
		t.Errorf("a file of one byte: %v", err)
	}
}

func TestBlockedAddresses(t *testing.T) {
	for _, tc := range []struct {
		addr    string
		blocked bool
	}{
		{"169.254.169.254", true}, {"169.254.0.1", true}, {"fe80::1", true}, {"fe80::1%eth0", true},
		{"fd00:ec2::254", true}, {"100.100.100.200", true}, {"::ffff:169.254.169.254", true},
		{"127.0.0.1", false}, {"10.0.0.5", false}, {"172.17.0.1", false}, {"192.168.1.10", false},
		{"100.64.0.1", false}, {"fd00::1", false}, {"93.184.216.34", false},
	} {
		if got := blockedAddress(netip.MustParseAddr(tc.addr)); got != tc.blocked {
			t.Errorf("blockedAddress(%s) = %v, want %v", tc.addr, got, tc.blocked)
		}
	}
}

// Webhooks reach private networks, where their receivers often run, but not the
// metadata services of clouds: neither an address of one in the URL, nor one that the
// name of the host resolves to, which the client refuses when it connects.
func TestWebhooksStayOffMetadataServices(t *testing.T) {
	ctx := context.Background()
	a := newApp(t)
	for _, url := range []string{"http://169.254.169.254/latest/meta-data/", "http://[fe80::1]/hook", "http://[fd00:ec2::254]/latest/meta-data/", "ftp://example.com/hook"} {
		if _, err := a.CreateWebhook(ctx, url); err == nil {
			t.Errorf("CreateWebhook(%s) succeeded", url)
		}
	}
	for _, url := range []string{"http://10.0.0.5/hook", "http://127.0.0.1:9000/hook", "http://app:3000/hooks/deneb"} {
		hook, err := a.CreateWebhook(ctx, url)
		if err != nil {
			t.Errorf("CreateWebhook(%s): %v", url, err)
			continue
		}
		a.store.DeleteWebhook(ctx, hook.ID)
	}

	_, err := a.client.Get("http://169.254.169.254/latest/meta-data/")
	var blocked blockedAddressError
	if !errors.As(err, &blocked) {
		t.Fatalf("a request to the metadata service: %v", err)
	}
	if reason := deliveryError(err); reason != blocked.Error() || strings.Contains(reason, "169.254") {
		t.Errorf("the reason of the delivery %q", reason)
	}
}

// failingMoves fails MoveFile for one key.
type failingMoves struct {
	storage.Storage
	key string
}

func (f failingMoves) MoveFile(ctx context.Context, key, local string) error {
	if key == f.key {
		return errors.New("the bucket did not answer")
	}
	return f.Storage.MoveFile(ctx, key, local)
}

func TestMoveAll(t *testing.T) {
	ctx := context.Background()
	a := newApp(t)
	dir := t.TempDir()
	files := map[string]string{}
	for i := range 20 {
		local := filepath.Join(dir, fmt.Sprintf("seg_%02d.ts", i))
		os.WriteFile(local, []byte(fmt.Sprint(i)), 0o644)
		files[fmt.Sprintf("videos/vid_a/hls/seg_%02d.ts", i)] = local
	}
	if err := a.moveAll(ctx, files); err != nil {
		t.Fatal(err)
	}
	for key, local := range files {
		if _, err := os.Stat(local); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s stays in the work folder", local)
		}
		if data, err := a.storage.ReadFile(ctx, key); err != nil || len(data) == 0 {
			t.Errorf("%s: %q, %v", key, data, err)
		}
	}

	os.WriteFile(filepath.Join(dir, "poster.jpg"), []byte("jpeg"), 0o644)
	a.storage = failingMoves{Storage: a.storage, key: "videos/vid_b/poster.jpg"}
	if err := a.moveAll(ctx, map[string]string{"videos/vid_b/poster.jpg": filepath.Join(dir, "poster.jpg")}); err == nil {
		t.Error("a move that failed returned no error")
	}
}
