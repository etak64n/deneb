package server

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/etak64n/deneb/internal/store"
	"github.com/tus/tusd/v2/pkg/filestore"
	"github.com/tus/tusd/v2/pkg/handler"
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
	s.locker.UseIn(composer)
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
	if !hook.Upload.SizeIsDeferred {
		if err := s.app.RoomForUpload(hook.Upload.Size); err != nil {
			return handler.HTTPResponse{}, handler.FileInfoChanges{}, handler.NewError("ERR_DISK_FULL", err.Error(), http.StatusInsufficientStorage)
		}
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
	_, err := s.app.UploadFinished(ctx, videoID, hook.Upload.Size)
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

// guardUploads checks the upload token on every request about the upload of a video,
// /v1/uploads/<video>?token=..., and takes the whole file there in one PUT. A tus
// client creates an upload with the token in its metadata, which checkUpload checks,
// and gets back the URL of the upload with the token, which it sends from then on. A
// video ID alone, which playback URLs show, is then not enough to touch an upload.
func (s *Server) guardUploads(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/uploads"), "/")
		if id == "" {
			if r.Method == http.MethodPost {
				w = &tokenLocation{ResponseWriter: w, token: uploadToken(r.Header.Get("Upload-Metadata"))}
			}
			next.ServeHTTP(w, r)
			return
		}
		if r.Method == http.MethodOptions {
			if r.Header.Get("Access-Control-Request-Method") != http.MethodPut {
				next.ServeHTTP(w, r)
				return
			}
			allowPut(w)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method == http.MethodPut {
			allowPut(w)
		}
		if !s.app.VerifyUploadToken(id, r.URL.Query().Get("token")) {
			writeError(w, http.StatusUnauthorized, "the upload URL is invalid or expired; POST /v1/videos returns a new one")
			return
		}
		if r.Method == http.MethodPut {
			s.putUpload(w, r, id)
			return
		}
		if r.Method == http.MethodDelete {
			// A finished upload is the file that the encoder reads, which a tus client may
			// not remove. Deleting the video removes it.
			if video, err := s.app.Store().Video(r.Context(), id); err == nil && video.Status != store.VideoWaiting {
				writeError(w, http.StatusConflict, "the upload of the video finished; delete the video instead")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// allowPut lets pages of any site send a file to an upload URL, which its token guards.
func allowPut(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "PUT")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Max-Age", "86400")
}

// uploadToken reads the token from the Upload-Metadata header of tus, whose pairs
// are a key and a value in base64.
func uploadToken(metadata string) string {
	for _, pair := range strings.Split(metadata, ",") {
		key, value, _ := strings.Cut(strings.TrimSpace(pair), " ")
		if key != "token" {
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(value)
		if err != nil {
			return ""
		}
		return string(decoded)
	}
	return ""
}

// tokenLocation adds the upload token to the URL of a new tus upload, which tusd
// builds from the ID alone.
type tokenLocation struct {
	http.ResponseWriter
	token string
}

func (t *tokenLocation) WriteHeader(status int) {
	if location := t.Header().Get("Location"); status == http.StatusCreated && location != "" && t.token != "" {
		t.Header().Set("Location", location+"?token="+url.QueryEscape(t.token))
	}
	t.ResponseWriter.WriteHeader(status)
}

func (t *tokenLocation) Unwrap() http.ResponseWriter { return t.ResponseWriter }

// putUpload stores the whole file of a video from the body of one request, then
// queues the video as a finished tus upload does.
func (s *Server) putUpload(w http.ResponseWriter, r *http.Request, id string) {
	// One upload of a video at a time. A second PUT would replace the file of the
	// first, and then remove it. The lock is that of tus, so a PUT also asks a tus
	// upload of the video that has not finished to stop, and takes its place.
	lock, err := s.locker.NewLock(id)
	if err != nil {
		s.fail(w, err)
		return
	}
	lockCtx, cancelLock := context.WithTimeout(r.Context(), 2*time.Second)
	err = lock.Lock(lockCtx, func() {})
	cancelLock()
	if err != nil {
		writeError(w, http.StatusConflict, "another upload of this video is in progress")
		return
	}
	defer lock.Unlock()

	video, err := s.app.Store().Video(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "the video does not exist")
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	if video.Status != store.VideoWaiting {
		writeError(w, http.StatusConflict, "the video already has its file")
		return
	}
	limit := s.app.Config().MaxUploadSize
	tooLarge := fmt.Sprintf("the file is larger than the %d GB that DENEB_MAX_UPLOAD_GB allows", limit/1_000_000_000)
	if r.ContentLength > limit {
		writeError(w, http.StatusRequestEntityTooLarge, tooLarge)
		return
	}
	// A request without a length is checked for the reserve alone.
	if err := s.app.RoomForUpload(r.ContentLength); err != nil {
		writeError(w, http.StatusInsufficientStorage, err.Error())
		return
	}
	// An unfinished tus upload of the video gives way to this one.
	s.app.RemoveUpload(id)
	path := s.app.UploadPath(id)
	// The file arrives under another name, so that a stopped request leaves no file
	// where the encoder looks.
	file, err := os.CreateTemp(filepath.Dir(path), id+".*.part")
	if err != nil {
		s.fail(w, err)
		return
	}
	size, copyErr := io.Copy(file, http.MaxBytesReader(w, r.Body, limit))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil || size == 0 {
		os.Remove(file.Name())
		var maxBytes *http.MaxBytesError
		switch {
		case errors.As(copyErr, &maxBytes):
			writeError(w, http.StatusRequestEntityTooLarge, tooLarge)
		case size == 0 && copyErr == nil && closeErr == nil:
			writeError(w, http.StatusBadRequest, "the request has no file; send it as the body")
		default:
			s.log.Warn("receive the file of a video", "video", id, "error", errors.Join(copyErr, closeErr))
			writeError(w, http.StatusBadRequest, "the file stopped before its end")
		}
		return
	}
	if err := os.Rename(file.Name(), path); err != nil {
		os.Remove(file.Name())
		s.fail(w, err)
		return
	}
	ctx := context.WithoutCancel(r.Context())
	// The answer shows the video as queued, though a worker may have taken it since.
	if video, err = s.app.UploadFinished(ctx, id, size); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			// The file goes only with its video. A video that has its file already keeps
			// it, as a finished tus upload does.
			if _, lookErr := s.app.Store().Video(ctx, id); errors.Is(lookErr, store.ErrNotFound) {
				s.app.RemoveUpload(id)
				writeError(w, http.StatusNotFound, "the video was deleted")
				return
			}
			writeError(w, http.StatusConflict, "the video has its file already")
			return
		}
		s.fail(w, err)
		return
	}
	s.log.Info("received the file of a video", "video", id, "bytes", size)
	writeJSON(w, http.StatusOK, map[string]any{"video": s.app.View(video, s.baseURL(r))})
}
