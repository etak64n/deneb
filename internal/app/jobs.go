package app

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/etak64n/deneb/internal/media"
	"github.com/etak64n/deneb/internal/store"
)

// webhookRetries are the waits before each new attempt of a delivery. A delivery
// that still fails after the last one gives up. Removing the files of a deleted video
// tries again on the same terms.
var webhookRetries = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 6 * time.Hour}

// mediaRetries are the waits before each new attempt of an encoding or captions job
// that failed for a reason that may pass, such as a bucket that did not answer: about
// seven hours in all, which outlasts most outages.
var mediaRetries = []time.Duration{time.Minute, 10 * time.Minute, time.Hour, 6 * time.Hour}

// roomWait is how long an encoding waits for free disk before it looks again.
const roomWait = 5 * time.Minute

// errStoppedTooOften is the failure of a job that deneb stopped in the middle of, as
// by crashing, more times than it retries a failure.
var errStoppedTooOften = errors.New("deneb stopped while it worked on this job more times than it retries a failure")

// lightWorkers deliver webhooks and remove files, so that a slow endpoint does not
// hold up the others.
const lightWorkers = 4

// deliveryHistory is how long deneb keeps the deliveries of webhooks that ended.
const deliveryHistory = 30 * 24 * time.Hour

// Version is the version of deneb, which webhook requests carry in their User-Agent.
var Version = "dev"

// Wake tells idle workers that a job is waiting, so that they do not wait for their next look.
func (a *App) Wake() {
	a.wakeMu.Lock()
	defer a.wakeMu.Unlock()
	for _, wake := range a.wakers {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
}

func (a *App) waker() chan struct{} {
	a.wakeMu.Lock()
	defer a.wakeMu.Unlock()
	wake := make(chan struct{}, 1)
	a.wakers = append(a.wakers, wake)
	return wake
}

// Run starts the workers: the encoders, those that deliver webhooks and remove files,
// and one that cleans up. It returns when ctx ends and every worker has stopped.
func (a *App) Run(ctx context.Context) {
	if count, err := a.store.RequeueRunningJobs(ctx, a.now()); err != nil {
		a.log.Error("queue the jobs that were running again", "error", err)
	} else if count > 0 {
		a.log.Info("queued again the jobs that were running when deneb stopped", "jobs", count)
	}
	a.recoverUploads(ctx)
	var wg sync.WaitGroup
	start := func(kinds ...string) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a.work(ctx, kinds)
		}()
	}
	for range a.cfg.Workers {
		start(store.JobTranscode, store.JobCaptions)
	}
	for range lightWorkers {
		start(store.JobWebhook, store.JobPurge)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		a.housekeep(ctx)
	}()
	wg.Wait()
}

// work takes jobs of the kinds one at a time until ctx ends.
func (a *App) work(ctx context.Context, kinds []string) {
	wake := a.waker()
	for ctx.Err() == nil {
		job, err := a.store.ClaimJob(ctx, kinds, a.now())
		if err != nil {
			if ctx.Err() == nil {
				a.log.Error("take a job", "error", err)
			}
			sleep(ctx, 5*time.Second)
			continue
		}
		if job == nil {
			select {
			case <-ctx.Done():
			case <-wake:
			case <-time.After(15 * time.Second):
			}
			continue
		}
		a.runJob(ctx, job)
	}
}

func (a *App) runJob(ctx context.Context, job *store.Job) {
	switch job.Kind {
	case store.JobWebhook:
		a.deliverJob(ctx, job)
		return
	case store.JobPurge:
		a.purgeJob(ctx, job)
		return
	}
	finish := context.WithoutCancel(ctx)
	// A crash or a kill in the middle of a job counts as an attempt, so that a job that
	// brings deneb down every time fails in the end rather than run forever.
	if job.Attempts > len(mediaRetries)+1 {
		a.log.Error("a job failed", "kind", job.Kind, "video", job.Ref, "attempt", job.Attempts, "error", errStoppedTooOften)
		a.failMedia(finish, job, errStoppedTooOften)
		return
	}
	// DeleteVideo stops the job through this context.
	jobCtx, cancel := context.WithCancel(ctx)
	a.runningMu.Lock()
	a.running[job.ID] = runningJob{video: job.Ref, cancel: cancel}
	a.runningMu.Unlock()
	var err error
	switch job.Kind {
	case store.JobTranscode:
		err = a.transcode(jobCtx, job.Ref)
	case store.JobCaptions:
		err = a.captions(jobCtx, job.Ref)
	default:
		err = permanentError{fmt.Errorf("unknown kind of job %q", job.Kind)}
	}
	a.runningMu.Lock()
	delete(a.running, job.ID)
	a.runningMu.Unlock()
	cancel()
	if _, lookErr := a.store.Video(finish, job.Ref); errors.Is(lookErr, store.ErrNotFound) {
		// The video was deleted while the job ran. What the job stored after the files
		// of the video were removed goes in another removal.
		if err := a.store.Enqueue(finish, store.JobPurge, job.Ref, a.now()); err != nil {
			a.log.Error("queue the removal of the files of a deleted video", "video", job.Ref, "error", err)
		}
		a.Wake()
		a.finishJob(finish, job)
		return
	}
	// A job that ended well is done, even when deneb began to stop meanwhile, so that
	// the next start does not do it again.
	if err == nil {
		a.finishJob(finish, job)
		return
	}
	if ctx.Err() != nil {
		// deneb is stopping, which stopped the job. It runs again at the next start, and
		// the stop does not count as an attempt.
		a.requeueStopped(finish, job)
		return
	}
	var room roomError
	if errors.As(err, &room) {
		a.log.Warn("an encoding waits for free disk", "video", job.Ref, "needBytes", room.need, "freeBytes", room.free)
		a.waitForRoom(finish, job, err)
		return
	}
	var permanent permanentError
	if job.Attempts <= len(mediaRetries) && !errors.As(err, &permanent) {
		a.log.Warn("a job failed and runs again", "kind", job.Kind, "video", job.Ref, "attempt", job.Attempts, "error", err)
		a.retryMedia(finish, job, err)
		return
	}
	a.log.Error("a job failed", "kind", job.Kind, "video", job.Ref, "attempt", job.Attempts, "error", err)
	a.failMedia(finish, job, err)
}

func (a *App) finishJob(ctx context.Context, job *store.Job) {
	if err := a.store.FinishJob(ctx, job.ID, a.now()); err != nil {
		a.log.Error("record a finished job", "job", job.ID, "error", err)
	}
}

// requeueStopped puts back in the queue a job that a stop of deneb interrupted, with
// its video, without counting the attempt.
func (a *App) requeueStopped(ctx context.Context, job *store.Job) {
	if err := a.store.RequeueJob(ctx, *job, a.now()); err != nil {
		a.log.Error("queue again a job that a stop interrupted", "job", job.ID, "error", err)
	}
}

// waitForRoom puts an encoding back in the queue to look for free disk again later,
// without counting the attempt. The video tells why it waits.
func (a *App) waitForRoom(ctx context.Context, job *store.Job, cause error) {
	if err := a.store.RequeueVideo(ctx, job.Ref, cause.Error(), a.now()); err != nil && !errors.Is(err, store.ErrNotFound) {
		a.log.Error("queue a video again", "video", job.Ref, "error", err)
	}
	if err := a.store.PostponeJob(ctx, job.ID, cause.Error(), a.now().Add(roomWait), a.now()); err != nil {
		a.log.Error("record a job that waits", "job", job.ID, "error", err)
	}
}

// retryMedia puts the video back in the queue and runs the job again later.
func (a *App) retryMedia(ctx context.Context, job *store.Job, cause error) {
	var err error
	if job.Kind == store.JobTranscode {
		err = a.store.RequeueVideo(ctx, job.Ref, cause.Error(), a.now())
	} else {
		err = a.store.SetCaptions(ctx, job.Ref, store.CaptionsQueued, "", a.now())
	}
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		a.log.Error("queue a video again", "video", job.Ref, "error", err)
	}
	if err := a.store.RetryJob(ctx, job.ID, cause.Error(), a.now().Add(mediaRetries[job.Attempts-1]), a.now()); err != nil {
		a.log.Error("record a failed job", "job", job.ID, "error", err)
	}
}

// failMedia records that the video, or its captions, failed for good.
func (a *App) failMedia(ctx context.Context, job *store.Job, cause error) {
	var err error
	if job.Kind == store.JobTranscode {
		if err = a.store.FailVideo(ctx, job.Ref, cause.Error(), a.now()); err == nil {
			a.emit(ctx, EventVideoFailed, job.Ref)
		}
		// A file that is not a video is of no use. Any other stays where it arrived, so
		// that POST /v1/videos/{id}/retry can encode it, until housekeep removes it a week
		// after it last changed.
		if errors.Is(cause, media.ErrNotAVideo) {
			a.RemoveUpload(job.Ref)
		}
	} else {
		err = a.store.SetCaptions(ctx, job.Ref, store.CaptionsFailed, "", a.now())
	}
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		a.log.Error("record a failed video", "video", job.Ref, "error", err)
	}
	if err := a.store.FailJob(ctx, job.ID, cause.Error(), a.now()); err != nil {
		a.log.Error("record a failed job", "job", job.ID, "error", err)
	}
}

// purgeJob removes the files of a deleted video: what arrived, what an encoder left,
// and what the storage holds. It tries again while the storage fails.
func (a *App) purgeJob(ctx context.Context, job *store.Job) {
	a.RemoveUpload(job.Ref)
	os.RemoveAll(a.workDir(job.Ref))
	err := a.storage.DeletePrefix(ctx, videoPrefix(job.Ref))
	finish := context.WithoutCancel(ctx)
	if ctx.Err() != nil {
		a.requeueStopped(finish, job) // The removal runs again at the next start.
		return
	}
	if err == nil {
		a.finishJob(finish, job)
		return
	}
	if job.Attempts > len(webhookRetries) {
		a.log.Error("gave up removing the files of a deleted video", "video", job.Ref, "error", err)
		a.store.FailJob(finish, job.ID, err.Error(), a.now())
		return
	}
	a.log.Warn("remove the files of a deleted video", "video", job.Ref, "attempt", job.Attempts, "error", err)
	a.store.RetryJob(finish, job.ID, err.Error(), a.now().Add(webhookRetries[job.Attempts-1]), a.now())
}

// deliverJob sends one delivery to its webhook, and plans the next attempt if it fails.
func (a *App) deliverJob(ctx context.Context, job *store.Job) {
	finish := context.WithoutCancel(ctx)
	delivery, hook, err := a.store.DeliveryWithWebhook(ctx, job.Ref)
	if errors.Is(err, store.ErrNotFound) {
		a.store.FinishJob(finish, job.ID, a.now()) // The webhook was removed.
		return
	}
	if err != nil {
		a.store.RetryJob(finish, job.ID, err.Error(), a.now().Add(time.Minute), a.now())
		return
	}
	status, err := a.post(ctx, hook, delivery)
	if ctx.Err() != nil {
		a.requeueStopped(finish, job) // The delivery runs again at the next start.
		return
	}
	if err == nil {
		a.store.RecordDelivery(finish, delivery.ID, store.DeliveryDelivered, status, "", a.now())
		a.store.FinishJob(finish, job.ID, a.now())
		return
	}
	// The deliveries keep why in words of deneb, and the log keeps the error itself.
	reason := deliveryError(err)
	if job.Attempts > len(webhookRetries) {
		a.log.Error("gave up a webhook delivery", "webhook", hook.ID, "event", delivery.Event, "attempt", job.Attempts, "error", withoutURL(err))
		a.store.RecordDelivery(finish, delivery.ID, store.DeliveryFailed, status, reason, a.now())
		a.store.FailJob(finish, job.ID, reason, a.now())
		return
	}
	a.log.Warn("a webhook delivery failed and runs again", "webhook", hook.ID, "event", delivery.Event, "attempt", job.Attempts, "error", withoutURL(err))
	a.store.RecordDelivery(finish, delivery.ID, store.DeliveryPending, status, reason, a.now())
	a.store.RetryJob(finish, job.ID, reason, a.now().Add(webhookRetries[job.Attempts-1]), a.now())
}

// post sends a delivery. The signature lets the receiver check that deneb sent it:
// Deneb-Signature is "t=<unix seconds>,v1=<HMAC-SHA256 of "<t>.<body>" with the secret>".
func (a *App) post(ctx context.Context, hook store.Webhook, delivery store.Delivery) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, hook.URL, bytes.NewBufferString(delivery.Payload))
	if err != nil {
		return 0, err
	}
	timestamp := strconv.FormatInt(a.now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(hook.Secret))
	mac.Write([]byte(timestamp + "." + delivery.Payload))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "deneb/"+Version)
	request.Header.Set("Deneb-Event", delivery.Event)
	request.Header.Set("Deneb-Delivery", delivery.ID)
	request.Header.Set("Deneb-Signature", "t="+timestamp+",v1="+hex.EncodeToString(mac.Sum(nil)))
	response, err := a.client.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return response.StatusCode, statusError{code: response.StatusCode}
	}
	return response.StatusCode, nil
}

// staleAfter is how long deneb keeps what was left unfinished: videos whose file
// never arrived, uploads that stopped, and the folders of encoders that stopped.
const staleAfter = 7 * 24 * time.Hour

// housekeep removes, every hour, expired sessions, old finished jobs, and what was left unfinished.
func (a *App) housekeep(ctx context.Context) {
	for {
		cutoff := a.now().Add(-staleAfter)
		if err := a.store.DeleteExpiredSessions(ctx, a.now()); err != nil && ctx.Err() == nil {
			a.log.Warn("delete expired sessions", "error", err)
		}
		if err := a.store.DeleteFinishedJobs(ctx, cutoff); err != nil && ctx.Err() == nil {
			a.log.Warn("delete old jobs", "error", err)
		}
		if err := a.store.DeleteOldDeliveries(ctx, a.now().Add(-deliveryHistory)); err != nil && ctx.Err() == nil {
			a.log.Warn("delete old deliveries of webhooks", "error", err)
		}
		if count, err := a.store.DeleteWaitingVideos(ctx, cutoff); err != nil && ctx.Err() == nil {
			a.log.Warn("delete videos whose file never arrived", "error", err)
		} else if count > 0 {
			a.log.Info("deleted videos whose file never arrived", "videos", count)
		}
		// An upload whose recording failed, as when the database was busy, is queued here.
		a.recoverUploads(ctx)
		// tusd keeps each upload as two files that change with every part that arrives.
		removeStale(a.log, filepath.Join(a.cfg.DataDir, "uploads"), cutoff)
		removeStale(a.log, filepath.Join(a.cfg.DataDir, "work"), cutoff)
		if !sleep(ctx, time.Hour) {
			return
		}
	}
}

// removeStale removes the entries of a folder that have not changed since cutoff.
func removeStale(log *slog.Logger, dir string, cutoff time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			log.Warn("remove a stale file", "path", filepath.Join(dir, entry.Name()), "error", err)
		}
	}
}

// sleep waits for d, and returns false if ctx ended first.
func sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
