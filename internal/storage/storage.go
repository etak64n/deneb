// Package storage keeps the files of the videos: their sources, HLS streams,
// posters, storyboards, and captions. The files live on the volume of the
// container, or in a bucket of an S3-compatible service.
package storage

import (
	"context"
	"net/http"
	"path"
)

// Storage is where the files of the videos live. Keys use slashes, such as
// "videos/<id>/hls/master.m3u8".
type Storage interface {
	// MoveFile stores a local file under a key, and removes the local file.
	MoveFile(ctx context.Context, key, localPath string) error
	// PutFile stores a copy of a local file under a key.
	PutFile(ctx context.Context, key, localPath string) error
	// LocalFile gives a local path of the object for the encoders: the file itself on
	// the volume, or a copy in dir that cleanup removes.
	LocalFile(ctx context.Context, key, dir string) (localPath string, cleanup func(), err error)
	// Serve answers a request for an object, with ranges. A bucket with redirects on
	// answers for segments and images with a redirect to a signed URL; playlists and
	// subtitles always come from deneb itself, so that the relative URLs inside them
	// keep pointing at deneb.
	Serve(w http.ResponseWriter, r *http.Request, key string)
	// ReadFile returns a small object, such as a WebVTT file, or an error that wraps
	// fs.ErrNotExist when there is none.
	ReadFile(ctx context.Context, key string) ([]byte, error)
	// DeletePrefix removes every object whose key starts with the prefix.
	DeletePrefix(ctx context.Context, prefix string) error
}

// ContentType returns the media type of an object by the extension of its key.
func ContentType(key string) string {
	switch path.Ext(key) {
	case ".m3u8":
		return "application/vnd.apple.mpegurl"
	case ".ts":
		return "video/mp2t"
	case ".m4s":
		return "video/iso.segment"
	case ".mp4":
		return "video/mp4"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".vtt":
		return "text/vtt; charset=utf-8"
	case ".json":
		return "application/json"
	}
	return "application/octet-stream"
}

// isText tells the objects that deneb serves itself rather than redirecting to.
func isText(key string) bool {
	switch path.Ext(key) {
	case ".m3u8", ".vtt":
		return true
	}
	return false
}
