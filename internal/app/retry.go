package app

import (
	"context"
	"errors"
	"os"

	"github.com/etak64n/deneb/internal/store"
)

// ErrNothingToRetry says that neither a video nor its captions failed.
var ErrNothingToRetry = errors.New("the video has not failed, nor have its captions")

// RetryVideo queues again a video whose encoding failed, from its file, or else the
// captions of a ready video that failed. It returns ErrNothingToRetry when neither
// failed, and ErrSourceGone when the file of a failed video is no longer there.
func (a *App) RetryVideo(ctx context.Context, id string) (store.Video, error) {
	video, err := a.store.Video(ctx, id)
	if err != nil {
		return store.Video{}, err
	}
	switch {
	case video.Status == store.VideoFailed:
		if _, statErr := os.Stat(a.UploadPath(id)); statErr != nil {
			stored, err := a.storage.Exists(ctx, sourceKey(id))
			if err != nil {
				return store.Video{}, err
			}
			if !stored {
				return store.Video{}, ErrSourceGone
			}
		}
		err = a.store.RetryVideo(ctx, id, a.now())
	case video.Status == store.VideoReady && video.CaptionsStatus == store.CaptionsFailed:
		err = a.store.RetryCaptions(ctx, id, a.now())
	default:
		return store.Video{}, ErrNothingToRetry
	}
	if err != nil {
		return store.Video{}, err
	}
	a.Wake()
	return a.store.Video(ctx, id)
}
