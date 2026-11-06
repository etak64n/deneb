package server

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"

	"github.com/etak64n/deneb/internal/store"
	"github.com/tus/tusd/v2/pkg/filestore"
	"github.com/tus/tusd/v2/pkg/handler"
	"github.com/tus/tusd/v2/pkg/memorylocker"
	expslog "golang.org/x/exp/slog"
)

// newTus returns the handler of resumable uploads in the tus protocol. A client
// creates an upload with the metadata that POST /v1/videos returned: the ID of the
// video and its upload token.
func (s *Server) newTus() (*handler.Handler, error) {
	dir := filepath.Join(s.app.Config().DataDir, "uploads")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	composer := handler.NewStoreComposer()
	filestore.New(dir).UseIn(composer)
	memorylocker.New().UseIn(composer)
	return handler.NewHandler(handler.Config{
		BasePath:                "/v1/uploads/",
		StoreComposer:           composer,
		MaxSize:                 s.app.Config().MaxUploadSize,
		RespectForwardedHeaders: true,
		DisableDownload:         true,
		DisableConcatenation:    true,
		// tusd logs every request at the info level; keep only its warnings and errors.
		Logger:                    expslog.New(expslog.NewJSONHandler(os.Stdout, &expslog.HandlerOptions{Level: expslog.LevelWarn})),
		PreUploadCreateCallback:   s.checkUpload,
		PreFinishResponseCallback: s.uploadFinished,
	})
}

// checkUpload accepts a new upload only with a valid token for a video that waits for its file.
func (s *Server) checkUpload(hook handler.HookEvent) (handler.HTTPResponse, handler.FileInfoChanges, error) {
	videoID := hook.Upload.MetaData["video_id"]
	if !s.app.VerifyUploadToken(videoID, hook.Upload.MetaData["token"]) {
		return handler.HTTPResponse{}, handler.FileInfoChanges{},
			handler.NewError("ERR_INVALID_UPLOAD_TOKEN", "the upload needs the video_id and token from POST /v1/videos, and the token expires after a day", http.StatusUnauthorized)
	}
	video, err := s.app.Store().Video(hook.Context, videoID)
	if errors.Is(err, store.ErrNotFound) {
		return handler.HTTPResponse{}, handler.FileInfoChanges{}, handler.NewError("ERR_VIDEO_NOT_FOUND", "the video does not exist", http.StatusNotFound)
	}
	if err != nil {
		return handler.HTTPResponse{}, handler.FileInfoChanges{}, err
	}
	if video.Status != store.VideoWaiting {
		return handler.HTTPResponse{}, handler.FileInfoChanges{}, handler.NewError("ERR_VIDEO_UPLOADED", "the video already has its file", http.StatusConflict)
	}
	// A video has one upload, under its own ID: a token cannot start many, and the
	// encoder finds the file by the video alone. An earlier upload that did not
	// finish, as when the client lost the answer to its POST, gives way to this one.
	s.app.RemoveUpload(videoID)
	// Keep the ID and the file name, and drop the token, which the upload no longer needs.
	return handler.HTTPResponse{}, handler.FileInfoChanges{ID: videoID, MetaData: handler.MetaData{
		"video_id": videoID,
		"filename": hook.Upload.MetaData["filename"],
	}}, nil
}

// uploadFinished queues the encoding of the video before tusd answers the last part
// of its file, so that a client never sees an upload succeed that deneb did not record.
func (s *Server) uploadFinished(hook handler.HookEvent) (handler.HTTPResponse, error) {
	videoID := hook.Upload.ID
	ctx := context.WithoutCancel(hook.Context)
	err := s.app.UploadFinished(ctx, videoID, hook.Upload.Size)
	if errors.Is(err, store.ErrNotFound) {
		// The video was deleted meanwhile, or already has its file.
		if _, lookErr := s.app.Store().Video(ctx, videoID); errors.Is(lookErr, store.ErrNotFound) {
			s.app.RemoveUpload(videoID)
			return handler.HTTPResponse{}, handler.NewError("ERR_VIDEO_NOT_FOUND", "the video was deleted", http.StatusNotFound)
		}
		return handler.HTTPResponse{}, nil
	}
	if err != nil {
		s.log.Error("queue an uploaded video", "video", videoID, "error", err)
		return handler.HTTPResponse{}, err
	}
	s.log.Info("received the file of a video", "video", videoID, "bytes", hook.Upload.Size)
	return handler.HTTPResponse{}, nil
}
