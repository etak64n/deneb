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
		NotifyCompleteUploads:   true,
		RespectForwardedHeaders: true,
		DisableDownload:         true,
		DisableConcatenation:    true,
		// tusd logs every request at the info level; keep only its warnings and errors.
		Logger:                  expslog.New(expslog.NewJSONHandler(os.Stdout, &expslog.HandlerOptions{Level: expslog.LevelWarn})),
		PreUploadCreateCallback: s.checkUpload,
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
	// Keep the ID and the file name, and drop the token, which the upload no longer needs.
	return handler.HTTPResponse{}, handler.FileInfoChanges{MetaData: handler.MetaData{
		"video_id": videoID,
		"filename": hook.Upload.MetaData["filename"],
	}}, nil
}

// RunUploads takes each upload that finishes, one at a time, until ctx ends. The
// first finished upload of a video becomes its source; any later one is removed.
func (s *Server) RunUploads(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-s.tus.CompleteUploads:
			s.finishUpload(ctx, event.Upload)
		}
	}
}

func (s *Server) finishUpload(ctx context.Context, upload handler.FileInfo) {
	binPath := upload.Storage[filestore.StorageKeyPath]
	infoPath := upload.Storage[filestore.StorageKeyInfoPath]
	defer os.Remove(infoPath)
	videoID := upload.MetaData["video_id"]
	log := s.log.With("video", videoID, "upload", upload.ID)

	video, err := s.app.Store().Video(ctx, videoID)
	if err != nil || video.Status != store.VideoWaiting {
		log.Warn("removed an upload whose video is gone or already has its file")
		os.Remove(binPath)
		return
	}
	if err := s.app.CompleteUpload(ctx, videoID, binPath, upload.Size); err != nil {
		log.Error("store an upload", "error", err)
		if failErr := s.app.Store().FailVideo(context.WithoutCancel(ctx), videoID, err.Error(), s.app.Now()); failErr != nil {
			log.Error("record a failed upload", "error", failErr)
		}
		os.Remove(binPath)
		return
	}
	log.Info("received the file of a video", "bytes", upload.Size)
}
