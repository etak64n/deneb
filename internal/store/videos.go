package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// The states of a video, from its creation to its playback.
const (
	VideoWaiting    = "waiting"
	VideoQueued     = "queued"
	VideoProcessing = "processing"
	VideoReady      = "ready"
	VideoFailed     = "failed"
)

// The states of the captions of a video.
const (
	CaptionsNone       = "none"
	CaptionsQueued     = "queued"
	CaptionsProcessing = "processing"
	CaptionsReady      = "ready"
	CaptionsFailed     = "failed"
)

// Rendition is one quality of the HLS stream of a video.
type Rendition struct {
	Name    string `json:"name"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Bitrate int    `json:"bitrate"`
}

// Video is a video and the state of its processing.
type Video struct {
	ID               string
	Title            string
	Status           string
	Error            string
	Progress         float64
	SourceSize       int64
	Duration         float64
	Width            int
	Height           int
	Renditions       []Rendition
	CaptionsStatus   string
	CaptionsLanguage string
	// Public lets anyone play the video under URLs that never expire.
	Public    bool
	CreatedAt time.Time
	UpdatedAt time.Time
	ReadyAt   *time.Time
}

const videoColumns = `id, title, status, error, progress, source_size, duration, width, height, renditions,
	captions_status, captions_language, public, created_at, updated_at, ready_at`

type scanner interface {
	Scan(dest ...any) error
}

func scanVideo(row scanner) (Video, error) {
	var video Video
	var renditions string
	var created, updated int64
	var ready sql.NullInt64
	err := row.Scan(&video.ID, &video.Title, &video.Status, &video.Error, &video.Progress, &video.SourceSize, &video.Duration,
		&video.Width, &video.Height, &renditions, &video.CaptionsStatus, &video.CaptionsLanguage, &video.Public, &created, &updated, &ready)
	if errors.Is(err, sql.ErrNoRows) {
		return Video{}, ErrNotFound
	}
	if err != nil {
		return Video{}, err
	}
	if err := json.Unmarshal([]byte(renditions), &video.Renditions); err != nil {
		return Video{}, err
	}
	video.CreatedAt = fromMillis(created)
	video.UpdatedAt = fromMillis(updated)
	video.ReadyAt = nullableTime(ready)
	return video, nil
}

// CreateVideo creates a video that waits for its upload.
func (s *Store) CreateVideo(ctx context.Context, id, title string, now time.Time) (Video, error) {
	_, err := s.db.ExecContext(ctx, `INSERT INTO videos (id, title, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		id, title, VideoWaiting, millis(now), millis(now))
	if err != nil {
		return Video{}, err
	}
	return s.Video(ctx, id)
}

// Video returns a video by its ID.
func (s *Store) Video(ctx context.Context, id string) (Video, error) {
	return scanVideo(s.db.QueryRowContext(ctx, `SELECT `+videoColumns+` FROM videos WHERE id = ?`, id))
}

// VideoCursor is where a page of videos starts: after the video of this time and ID.
type VideoCursor struct {
	CreatedAt time.Time
	ID        string
}

// ListVideos returns up to limit videos, the newest first, after the cursor if any.
func (s *Store) ListVideos(ctx context.Context, after *VideoCursor, limit int) ([]Video, error) {
	query := `SELECT ` + videoColumns + ` FROM videos`
	args := []any{}
	if after != nil {
		query += ` WHERE (created_at, id) < (?, ?)`
		args = append(args, millis(after.CreatedAt), after.ID)
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	videos := []Video{}
	for rows.Next() {
		video, err := scanVideo(rows)
		if err != nil {
			return nil, err
		}
		videos = append(videos, video)
	}
	return videos, rows.Err()
}

// SetVideoTitle renames a video.
func (s *Store) SetVideoTitle(ctx context.Context, id, title string, now time.Time) error {
	return expectRow(s.db.ExecContext(ctx, `UPDATE videos SET title = ?, updated_at = ? WHERE id = ?`, title, millis(now), id))
}

// SetVideoPublic makes a video public, so that it plays under URLs that never expire,
// or makes it play under signed URLs only, which ends its public URLs.
func (s *Store) SetVideoPublic(ctx context.Context, id string, public bool, now time.Time) error {
	value := 0
	if public {
		value = 1
	}
	return expectRow(s.db.ExecContext(ctx, `UPDATE videos SET public = ?, updated_at = ? WHERE id = ?`, value, millis(now), id))
}

// QueueVideo records that the upload of a video finished, and queues its encoding,
// in one transaction so that a video is never queued without its job.
func (s *Store) QueueVideo(ctx context.Context, id string, sourceSize int64, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := expectRow(tx.ExecContext(ctx,
		`UPDATE videos SET status = ?, source_size = ?, error = '', progress = 0, updated_at = ? WHERE id = ? AND status = ?`,
		VideoQueued, sourceSize, millis(now), id, VideoWaiting)); err != nil {
		return err
	}
	if err := enqueue(ctx, tx, JobTranscode, id, now); err != nil {
		return err
	}
	return tx.Commit()
}

// StartVideo marks a video as processing.
// RequeueVideo puts a video whose encoding failed for a reason that may pass back in
// the queue, with the reason, until its job runs again.
func (s *Store) RequeueVideo(ctx context.Context, id, reason string, now time.Time) error {
	return expectRow(s.db.ExecContext(ctx, `UPDATE videos SET status = ?, progress = 0, error = ?, updated_at = ? WHERE id = ?`,
		VideoQueued, reason, millis(now), id))
}

func (s *Store) StartVideo(ctx context.Context, id string, now time.Time) error {
	return expectRow(s.db.ExecContext(ctx, `UPDATE videos SET status = ?, progress = 0, error = '', updated_at = ? WHERE id = ?`,
		VideoProcessing, millis(now), id))
}

// SetVideoProgress records how far the encoding of a video got, from 0 to 1.
func (s *Store) SetVideoProgress(ctx context.Context, id string, progress float64, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE videos SET progress = ?, updated_at = ? WHERE id = ? AND status = ?`,
		progress, millis(now), id, VideoProcessing)
	return err
}

// ReadyVideo records the result of an encoding that succeeded. With captions, it
// queues their job in the same transaction, so that queued captions always have one.
func (s *Store) ReadyVideo(ctx context.Context, id string, duration float64, width, height int, renditions []Rendition, captions bool, now time.Time) error {
	encoded, err := json.Marshal(renditions)
	if err != nil {
		return err
	}
	captionsStatus := CaptionsNone
	if captions {
		captionsStatus = CaptionsQueued
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := expectRow(tx.ExecContext(ctx,
		`UPDATE videos SET status = ?, progress = 1, duration = ?, width = ?, height = ?, renditions = ?, captions_status = ?,
		 updated_at = ?, ready_at = ? WHERE id = ?`,
		VideoReady, duration, width, height, string(encoded), captionsStatus, millis(now), millis(now), id)); err != nil {
		return err
	}
	if captions {
		if err := enqueue(ctx, tx, JobCaptions, id, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteWaitingVideos removes the videos whose file never arrived, created before a time.
func (s *Store) DeleteWaitingVideos(ctx context.Context, before time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM videos WHERE status = ? AND created_at < ?`, VideoWaiting, millis(before))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// FailVideo records why the processing of a video failed.
func (s *Store) FailVideo(ctx context.Context, id, reason string, now time.Time) error {
	return expectRow(s.db.ExecContext(ctx, `UPDATE videos SET status = ?, error = ?, updated_at = ? WHERE id = ?`,
		VideoFailed, reason, millis(now), id))
}

// SetCaptions records the state of the captions of a video, and their language once known.
func (s *Store) SetCaptions(ctx context.Context, id, status, language string, now time.Time) error {
	return expectRow(s.db.ExecContext(ctx,
		`UPDATE videos SET captions_status = ?, captions_language = CASE WHEN ? = '' THEN captions_language ELSE ? END, updated_at = ? WHERE id = ?`,
		status, language, language, millis(now), id))
}

// DeleteVideo removes a video and the jobs that wait to work on it.
func (s *Store) DeleteVideo(ctx context.Context, id string, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := expectRow(tx.ExecContext(ctx, `DELETE FROM videos WHERE id = ?`, id)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM jobs WHERE ref = ? AND kind IN (?, ?) AND status = ?`, id, JobTranscode, JobCaptions, JobQueued); err != nil {
		return err
	}
	// The files go in a job of their own, which tries again until the storage answers.
	if err := enqueue(ctx, tx, JobPurge, id, now); err != nil {
		return err
	}
	return tx.Commit()
}
