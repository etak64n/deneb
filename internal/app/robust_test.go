package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/etak64n/deneb/internal/config"
	"github.com/etak64n/deneb/internal/media"
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

func TestWebhookAddresses(t *testing.T) {
	for _, tc := range []struct {
		addr     string
		internal bool
	}{
		{"93.184.216.34", false}, {"2606:4700:4700::1111", false},
		{"127.0.0.1", true}, {"::1", true}, {"10.1.2.3", true}, {"172.17.0.1", true}, {"192.168.1.10", true},
		{"169.254.169.254", true}, {"fe80::1", true}, {"fc00::1", true}, {"100.64.0.1", true},
		{"0.0.0.0", true}, {"0.1.2.3", true}, {"224.0.0.1", true}, {"::ffff:10.0.0.1", true},
	} {
		if got := internalAddress(netip.MustParseAddr(tc.addr)); got != tc.internal {
			t.Errorf("internalAddress(%s) = %v, want %v", tc.addr, got, tc.internal)
		}
	}
	allow := []netip.Prefix{netip.MustParsePrefix("172.16.0.0/12")}
	if !webhookAllowed(netip.MustParseAddr("172.17.0.1"), allow) || webhookAllowed(netip.MustParseAddr("10.0.0.5"), allow) {
		t.Error("the allowed networks are not the ones listed")
	}
}

// Webhooks reach no private or local address that the configuration does not list:
// neither one in the URL, nor one that the name of the host resolves to.
func TestWebhooksStayOutOfPrivateNetworks(t *testing.T) {
	ctx := context.Background()
	a := newAppWith(t, func(cfg *config.Config) { cfg.WebhookAllowNetworks = nil })
	for _, url := range []string{"http://10.0.0.5/hook", "http://[::1]:8080/hook", "http://localhost:9000/hook", "ftp://example.com/hook"} {
		if _, err := a.CreateWebhook(ctx, url); err == nil {
			t.Errorf("CreateWebhook(%s) succeeded", url)
		}
	}
	public, err := a.CreateWebhook(ctx, "https://hooks.example.com/deneb")
	if err != nil {
		t.Errorf("a public name: %v", err)
	}
	// No delivery of this test goes out to the internet.
	a.store.DeleteWebhook(ctx, public.ID)

	reached := make(chan struct{}, 1)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached <- struct{}{}
	}))
	defer receiver.Close()
	// The store takes the URL as it is, as it would a name that resolves to the loopback.
	hook, err := a.store.CreateWebhook(ctx, NewID("wh"), receiver.URL+"/hook", "whsec_test", a.now())
	if err != nil {
		t.Fatal(err)
	}
	a.store.CreateVideo(ctx, "vid_a", "Talk", a.now())
	a.emit(ctx, EventVideoReady, "vid_a")
	runWorkers(t, a)
	waitFor(t, "the delivery to fail", func() bool {
		deliveries, _ := a.store.ListDeliveries(ctx, hook.ID, 10)
		return len(deliveries) == 1 && deliveries[0].LastError != ""
	})
	deliveries, _ := a.store.ListDeliveries(ctx, hook.ID, 10)
	if !strings.Contains(deliveries[0].LastError, "private or local network") || strings.Contains(deliveries[0].LastError, "127.0.0.1") {
		t.Errorf("last error %q", deliveries[0].LastError)
	}
	select {
	case <-reached:
		t.Error("a webhook reached the loopback")
	default:
	}
}
