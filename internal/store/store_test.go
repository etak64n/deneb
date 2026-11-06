package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "deneb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

var start = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

func TestOpenTwiceKeepsData(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "deneb.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting(ctx, "signing_secret", "one"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if value, found, err := s.Setting(ctx, "signing_secret"); err != nil || !found || value != "one" {
		t.Errorf("setting %q, %v, %v", value, found, err)
	}
}

func TestVideoLifecycle(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	video, err := s.CreateVideo(ctx, "vid_a", "Talk", start)
	if err != nil {
		t.Fatal(err)
	}
	if video.Status != VideoWaiting || video.CaptionsStatus != CaptionsNone || len(video.Renditions) != 0 {
		t.Errorf("new video %+v", video)
	}
	if err := s.QueueVideo(ctx, "vid_a", 1234, start); err != nil {
		t.Fatal(err)
	}
	// A second upload of the same video does not queue it again.
	if err := s.QueueVideo(ctx, "vid_a", 1234, start); !errors.Is(err, ErrNotFound) {
		t.Errorf("second queue: %v", err)
	}
	job, err := s.ClaimJob(ctx, []string{JobTranscode, JobCaptions}, start)
	if err != nil || job == nil || job.Kind != JobTranscode || job.Ref != "vid_a" || job.Attempts != 1 {
		t.Fatalf("job %+v, %v", job, err)
	}
	if again, err := s.ClaimJob(ctx, []string{JobTranscode}, start); err != nil || again != nil {
		t.Errorf("a running job was claimed again: %+v, %v", again, err)
	}
	if err := s.StartVideo(ctx, "vid_a", start); err != nil {
		t.Fatal(err)
	}
	if err := s.SetVideoProgress(ctx, "vid_a", 0.5, start); err != nil {
		t.Fatal(err)
	}
	renditions := []Rendition{{Name: "720p", Width: 1280, Height: 720, Bitrate: 2_928_000}}
	if err := s.ReadyVideo(ctx, "vid_a", 12.5, 1280, 720, renditions, true, start.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	video, err = s.Video(ctx, "vid_a")
	if err != nil {
		t.Fatal(err)
	}
	if video.Status != VideoReady || video.Progress != 1 || video.SourceSize != 1234 || video.Duration != 12.5 ||
		video.CaptionsStatus != CaptionsQueued || video.ReadyAt == nil || len(video.Renditions) != 1 || video.Renditions[0] != renditions[0] {
		t.Errorf("ready video %+v", video)
	}
	// Making the video ready queued its captions in the same step.
	captions, err := s.ClaimJob(ctx, []string{JobCaptions}, start.Add(time.Minute))
	if err != nil || captions == nil || captions.Ref != "vid_a" {
		t.Fatalf("captions job %+v, %v", captions, err)
	}
	if err := s.SetCaptions(ctx, "vid_a", CaptionsReady, "ja", start); err != nil {
		t.Fatal(err)
	}
	// An empty language keeps the one that is known.
	if err := s.SetCaptions(ctx, "vid_a", CaptionsReady, "", start); err != nil {
		t.Fatal(err)
	}
	if video, _ := s.Video(ctx, "vid_a"); video.CaptionsStatus != CaptionsReady || video.CaptionsLanguage != "ja" {
		t.Errorf("captions %q, %q", video.CaptionsStatus, video.CaptionsLanguage)
	}
}

func TestDeleteVideoRemovesQueuedJobs(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	s.CreateVideo(ctx, "vid_a", "", start)
	s.QueueVideo(ctx, "vid_a", 1, start)
	if err := s.DeleteVideo(ctx, "vid_a", start); err != nil {
		t.Fatal(err)
	}
	if job, err := s.ClaimJob(ctx, []string{JobTranscode}, start); err != nil || job != nil {
		t.Errorf("the job of a deleted video remains: %+v, %v", job, err)
	}
	if job, err := s.ClaimJob(ctx, []string{JobPurge}, start); err != nil || job == nil || job.Ref != "vid_a" {
		t.Errorf("no job removes the files of the deleted video: %+v, %v", job, err)
	}
	if _, err := s.Video(ctx, "vid_a"); !errors.Is(err, ErrNotFound) {
		t.Errorf("video: %v", err)
	}
	if err := s.DeleteVideo(ctx, "vid_a", start); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete: %v", err)
	}
}

func TestClaimJobOrderAndKinds(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	s.Enqueue(ctx, JobWebhook, "whd_1", start)
	s.Enqueue(ctx, JobTranscode, "vid_1", start.Add(time.Second))
	s.Enqueue(ctx, JobTranscode, "vid_2", start)
	// A job for later is not due yet.
	enqueue(ctx, s.db, JobTranscode, "vid_3", start.Add(time.Hour))

	var refs []string
	for {
		job, err := s.ClaimJob(ctx, []string{JobTranscode}, start.Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if job == nil {
			break
		}
		refs = append(refs, job.Ref)
	}
	if fmt.Sprint(refs) != "[vid_2 vid_1]" {
		t.Errorf("claimed %v", refs)
	}
	if job, _ := s.ClaimJob(ctx, []string{JobWebhook}, start); job == nil || job.Ref != "whd_1" {
		t.Errorf("webhook job %+v", job)
	}
}

func TestRetryAndRequeue(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	s.CreateVideo(ctx, "vid_a", "", start)
	s.QueueVideo(ctx, "vid_a", 1, start)
	job, _ := s.ClaimJob(ctx, []string{JobTranscode}, start)
	s.StartVideo(ctx, "vid_a", start)

	// deneb stopped while the job ran: the next start queues it again.
	count, err := s.RequeueRunningJobs(ctx, start)
	if err != nil || count != 1 {
		t.Fatalf("requeued %d, %v", count, err)
	}
	if video, _ := s.Video(ctx, "vid_a"); video.Status != VideoQueued {
		t.Errorf("status %q", video.Status)
	}
	// The stop does not count as an attempt.
	again, _ := s.ClaimJob(ctx, []string{JobTranscode}, start)
	if again == nil || again.ID != job.ID || again.Attempts != 1 {
		t.Fatalf("job %+v", again)
	}

	if err := s.RetryJob(ctx, again.ID, "busy", start.Add(time.Minute), start); err != nil {
		t.Fatal(err)
	}
	if early, _ := s.ClaimJob(ctx, []string{JobTranscode}, start.Add(30*time.Second)); early != nil {
		t.Errorf("a retry ran before its time: %+v", early)
	}
	if due, _ := s.ClaimJob(ctx, []string{JobTranscode}, start.Add(time.Minute)); due == nil {
		t.Error("a retry did not run at its time")
	}
}

func TestListVideosPages(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	// Two videos share a time, so that the ID orders them.
	for i, at := range []time.Time{start, start.Add(time.Second), start.Add(time.Second), start.Add(2 * time.Second)} {
		if _, err := s.CreateVideo(ctx, fmt.Sprintf("vid_%d", i), "", at); err != nil {
			t.Fatal(err)
		}
	}
	var seen []string
	var after *VideoCursor
	for {
		page, err := s.ListVideos(ctx, after, 2)
		if err != nil {
			t.Fatal(err)
		}
		for _, video := range page {
			seen = append(seen, video.ID)
		}
		if len(page) < 2 {
			break
		}
		last := page[len(page)-1]
		after = &VideoCursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	if fmt.Sprint(seen) != "[vid_3 vid_2 vid_1 vid_0]" {
		t.Errorf("pages %v", seen)
	}
}

func TestDeleteWaitingVideos(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	s.CreateVideo(ctx, "vid_old", "", start)
	s.CreateVideo(ctx, "vid_new", "", start.Add(48*time.Hour))
	s.CreateVideo(ctx, "vid_queued", "", start)
	s.QueueVideo(ctx, "vid_queued", 1, start)
	count, err := s.DeleteWaitingVideos(ctx, start.Add(time.Hour))
	if err != nil || count != 1 {
		t.Fatalf("deleted %d, %v", count, err)
	}
	if _, err := s.Video(ctx, "vid_old"); !errors.Is(err, ErrNotFound) {
		t.Error("the old waiting video remains")
	}
	for _, id := range []string{"vid_new", "vid_queued"} {
		if _, err := s.Video(ctx, id); err != nil {
			t.Errorf("%s: %v", id, err)
		}
	}
}

func TestAccounts(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	if _, err := s.CreateFirstUser(ctx, "usr_1", "admin@example.com", "hash", start); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateFirstUser(ctx, "usr_2", "other@example.com", "hash", start); err == nil {
		t.Error("a second administrator was created")
	}
	if count, _ := s.CountUsers(ctx); count != 1 {
		t.Errorf("%d users", count)
	}

	s.CreateSession(ctx, "token-hash", "usr_1", start.Add(time.Hour))
	if user, err := s.SessionUser(ctx, "token-hash", start); err != nil || user.Email != "admin@example.com" {
		t.Errorf("session user %+v, %v", user, err)
	}
	if _, err := s.SessionUser(ctx, "token-hash", start.Add(2*time.Hour)); !errors.Is(err, ErrNotFound) {
		t.Errorf("an expired session signed in: %v", err)
	}
	s.DeleteExpiredSessions(ctx, start.Add(2*time.Hour))
	if _, err := s.SessionUser(ctx, "token-hash", start); !errors.Is(err, ErrNotFound) {
		t.Errorf("the expired session remains: %v", err)
	}

	if _, err := s.CreateAPIKey(ctx, "key_1", "CI", "dnb_abcdef", "secret-hash", start); err != nil {
		t.Fatal(err)
	}
	key, err := s.UseAPIKey(ctx, "secret-hash", start)
	if err != nil || key.ID != "key_1" || key.LastUsedAt == nil || !key.LastUsedAt.Equal(start) {
		t.Fatalf("key %+v, %v", key, err)
	}
	// Within a minute, the recorded time of use stays.
	key, _ = s.UseAPIKey(ctx, "secret-hash", start.Add(30*time.Second))
	if !key.LastUsedAt.Equal(start) {
		t.Errorf("last used %v", key.LastUsedAt)
	}
	key, _ = s.UseAPIKey(ctx, "secret-hash", start.Add(2*time.Minute))
	if !key.LastUsedAt.Equal(start.Add(2 * time.Minute)) {
		t.Errorf("last used %v", key.LastUsedAt)
	}
	if _, err := s.UseAPIKey(ctx, "other-hash", start); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown key: %v", err)
	}
	s.DeleteAPIKey(ctx, "key_1")
	if _, err := s.UseAPIKey(ctx, "secret-hash", start); !errors.Is(err, ErrNotFound) {
		t.Errorf("a revoked key works: %v", err)
	}
}

func TestWebhookDeliveries(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	s.CreateWebhook(ctx, "wh_1", "https://example.com/hook", "whsec_1", start)
	s.CreateWebhook(ctx, "wh_2", "https://example.com/other", "whsec_2", start)
	n := 0
	newID := func() string { n++; return fmt.Sprintf("whd_%d", n) }
	if err := s.QueueEvent(ctx, "video.ready", `{"type":"video.ready"}`, newID, start); err != nil {
		t.Fatal(err)
	}
	deliveryOf := func(webhookID string) Delivery {
		t.Helper()
		deliveries, err := s.ListDeliveries(ctx, webhookID, 10)
		if err != nil || len(deliveries) != 1 {
			t.Fatalf("deliveries of %s: %+v, %v", webhookID, deliveries, err)
		}
		return deliveries[0]
	}
	first, second := deliveryOf("wh_1"), deliveryOf("wh_2")
	for _, delivery := range []Delivery{first, second} {
		got, hook, err := s.DeliveryWithWebhook(ctx, delivery.ID)
		if err != nil || got.Status != DeliveryPending || got.Payload != `{"type":"video.ready"}` || hook.ID != delivery.WebhookID {
			t.Errorf("%s: %+v, %+v, %v", delivery.ID, got, hook, err)
		}
		if job, _ := s.ClaimJob(ctx, []string{JobWebhook}, start); job == nil {
			t.Errorf("no job for %s", delivery.ID)
		}
	}
	if err := s.RecordDelivery(ctx, first.ID, DeliveryDelivered, 200, "", start); err != nil {
		t.Fatal(err)
	}
	if delivered := deliveryOf("wh_1"); delivered.Status != DeliveryDelivered || delivered.Attempts != 1 || delivered.ResponseStatus != 200 || delivered.DeliveredAt == nil {
		t.Errorf("delivered %+v", delivered)
	}
	// Removing a webhook removes its deliveries.
	if err := s.DeleteWebhook(ctx, "wh_2"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.DeliveryWithWebhook(ctx, second.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("delivery of a removed webhook: %v", err)
	}
}

func TestVideoPublic(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	if video, _ := s.CreateVideo(ctx, "vid_a", "", start); video.Public {
		t.Error("a new video is public")
	}
	if err := s.SetVideoPublic(ctx, "vid_a", true, start); err != nil {
		t.Fatal(err)
	}
	if video, _ := s.Video(ctx, "vid_a"); !video.Public {
		t.Error("the video did not become public")
	}
	if err := s.SetVideoPublic(ctx, "vid_a", false, start); err != nil {
		t.Fatal(err)
	}
	if video, _ := s.Video(ctx, "vid_a"); video.Public {
		t.Error("the video stayed public")
	}
	if err := s.SetVideoPublic(ctx, "vid_missing", true, start); !errors.Is(err, ErrNotFound) {
		t.Errorf("a missing video: %v", err)
	}
}
