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

	"github.com/etak64n/deneb/internal/store"
)

// webhookRetries are the waits before each new attempt of a delivery. A delivery
// that still fails after the last one gives up.
var webhookRetries = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 6 * time.Hour}

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

// Run starts the workers: the encoders, one that delivers webhooks, and one that
// cleans up. It returns when ctx ends and every worker has stopped.
func (a *App) Run(ctx context.Context) {
	if count, err := a.store.RequeueRunningJobs(ctx, a.now()); err != nil {
		a.log.Error("queue the jobs that were running again", "error", err)
	} else if count > 0 {
		a.log.Info("queued again the jobs that were running when deneb stopped", "jobs", count)
	}
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
	start(store.JobWebhook)
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
	if job.Kind == store.JobWebhook {
		a.deliverJob(ctx, job)
		return
	}
	// DeleteVideo stops the job through this context.
	jobCtx, cancel := context.WithCancel(ctx)
	a.cancelsMu.Lock()
	a.cancels[job.Ref] = cancel
	a.cancelsMu.Unlock()
	var err error
	switch job.Kind {
	case store.JobTranscode:
		err = a.transcode(jobCtx, job.Ref)
	case store.JobCaptions:
		err = a.captions(jobCtx, job.Ref)
	default:
		err = fmt.Errorf("unknown kind of job %q", job.Kind)
	}
	a.cancelsMu.Lock()
	delete(a.cancels, job.Ref)
	a.cancelsMu.Unlock()
	cancel()
	if ctx.Err() != nil {
		// deneb is stopping. The job stays running and runs again at the next start.
		return
	}
	finish := context.WithoutCancel(ctx)
	if _, lookErr := a.store.Video(finish, job.Ref); errors.Is(lookErr, store.ErrNotFound) {
		// The video was deleted while the job ran. Remove what the job stored after
		// DeleteVideo removed the files.
		if delErr := a.storage.DeletePrefix(finish, videoPrefix(job.Ref)); delErr != nil {
			a.log.Error("delete the files of a deleted video", "video", job.Ref, "error", delErr)
		}
		err = nil
	}
	if err != nil {
		if failErr := a.store.FailJob(finish, job.ID, err.Error(), a.now()); failErr != nil {
			a.log.Error("record a failed job", "job", job.ID, "error", failErr)
		}
		return
	}
	if err := a.store.FinishJob(finish, job.ID, a.now()); err != nil {
		a.log.Error("record a finished job", "job", job.ID, "error", err)
	}
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
	if err == nil {
		a.store.RecordDelivery(finish, delivery.ID, store.DeliveryDelivered, status, "", a.now())
		a.store.FinishJob(finish, job.ID, a.now())
		return
	}
	if job.Attempts > len(webhookRetries) {
		a.log.Warn("gave up a webhook delivery", "webhook", hook.ID, "event", delivery.Event, "error", err)
		a.store.RecordDelivery(finish, delivery.ID, store.DeliveryFailed, status, err.Error(), a.now())
		a.store.FailJob(finish, job.ID, err.Error(), a.now())
		return
	}
	a.store.RecordDelivery(finish, delivery.ID, store.DeliveryPending, status, err.Error(), a.now())
	a.store.RetryJob(finish, job.ID, err.Error(), a.now().Add(webhookRetries[job.Attempts-1]), a.now())
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
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return response.StatusCode, fmt.Errorf("the webhook answered %s", response.Status)
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
		if count, err := a.store.DeleteWaitingVideos(ctx, cutoff); err != nil && ctx.Err() == nil {
			a.log.Warn("delete videos whose file never arrived", "error", err)
		} else if count > 0 {
			a.log.Info("deleted videos whose file never arrived", "videos", count)
		}
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
