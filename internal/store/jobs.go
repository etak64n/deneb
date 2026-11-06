package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// The kinds of background jobs.
const (
	JobTranscode = "transcode"
	JobCaptions  = "captions"
	JobWebhook   = "webhook"
	// JobPurge removes the files of a deleted video.
	JobPurge = "purge"
)

// The states of a job.
const (
	JobQueued  = "queued"
	JobRunning = "running"
	JobDone    = "done"
	JobFailed  = "failed"
)

// Job is a piece of background work.
type Job struct {
	ID       int64
	Kind     string
	Ref      string
	Attempts int
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func enqueue(ctx context.Context, db execer, kind, ref string, runAfter time.Time) error {
	_, err := db.ExecContext(ctx, `INSERT INTO jobs (kind, ref, status, run_after, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		kind, ref, JobQueued, millis(runAfter), millis(runAfter), millis(runAfter))
	return err
}

// Enqueue adds a job that runs now.
func (s *Store) Enqueue(ctx context.Context, kind, ref string, now time.Time) error {
	return enqueue(ctx, s.db, kind, ref, now)
}

// ClaimJob takes the oldest queued job of the kinds that is due, and marks it as running.
// It returns nil when no job is waiting.
func (s *Store) ClaimJob(ctx context.Context, kinds []string, now time.Time) (*Job, error) {
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(kinds)), ",")
	args := []any{JobRunning, millis(now), JobQueued, millis(now)}
	for _, kind := range kinds {
		args = append(args, kind)
	}
	var job Job
	err := s.db.QueryRowContext(ctx,
		`UPDATE jobs SET status = ?, attempts = attempts + 1, updated_at = ?
		 WHERE id = (SELECT id FROM jobs WHERE status = ? AND run_after <= ? AND kind IN (`+placeholders+`) ORDER BY run_after, id LIMIT 1)
		 RETURNING id, kind, ref, attempts`, args...).Scan(&job.ID, &job.Kind, &job.Ref, &job.Attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &job, nil
}

// FinishJob records that a job succeeded.
func (s *Store) FinishJob(ctx context.Context, id int64, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE jobs SET status = ?, error = '', updated_at = ? WHERE id = ?`, JobDone, millis(now), id)
	return err
}

// FailJob records that a job failed for good.
func (s *Store) FailJob(ctx context.Context, id int64, reason string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE jobs SET status = ?, error = ?, updated_at = ? WHERE id = ?`, JobFailed, reason, millis(now), id)
	return err
}

// RetryJob queues a job that failed again, to run after a delay.
func (s *Store) RetryJob(ctx context.Context, id int64, reason string, runAfter, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE jobs SET status = ?, error = ?, run_after = ?, updated_at = ? WHERE id = ?`,
		JobQueued, reason, millis(runAfter), millis(now), id)
	return err
}

// RequeueRunningJobs queues again the jobs that were running when deneb stopped,
// and puts their videos back in the queue. A stop does not count as an attempt.
func (s *Store) RequeueRunningJobs(ctx context.Context, now time.Time) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE jobs SET status = ?, attempts = max(attempts - 1, 0), updated_at = ? WHERE status = ?`,
		JobQueued, millis(now), JobRunning)
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE videos SET status = ?, progress = 0, updated_at = ? WHERE status = ?`, VideoQueued, millis(now), VideoProcessing); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE videos SET captions_status = ?, updated_at = ? WHERE captions_status = ?`, CaptionsQueued, millis(now), CaptionsProcessing); err != nil {
		return 0, err
	}
	count, _ := result.RowsAffected()
	return count, tx.Commit()
}

// DeleteFinishedJobs removes the jobs that ended before a time.
func (s *Store) DeleteFinishedJobs(ctx context.Context, before time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM jobs WHERE status IN (?, ?) AND updated_at < ?`, JobDone, JobFailed, millis(before))
	return err
}
