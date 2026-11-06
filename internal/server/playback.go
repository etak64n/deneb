package server

import (
	"errors"
	"net/http"
	"path"

	"github.com/etak64n/deneb/internal/app"
	"github.com/etak64n/deneb/internal/store"
)

// allowAnyOrigin lets players on any site read the stream, which the signed URL already guards.
func allowAnyOrigin(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Range")
	w.Header().Set("Access-Control-Expose-Headers", "Content-Length, Content-Range")
}

func (s *Server) playOptions(w http.ResponseWriter, r *http.Request) {
	allowAnyOrigin(w)
	w.WriteHeader(http.StatusNoContent)
}

// play serves the files of a ready video under a signed URL:
// /play/<video>/<token>/hls/master.m3u8, its playlists and segments, poster.jpg,
// storyboard.vtt and storyboard.jpg, captions.vtt, and info.json for the embed page.
// The token sits in the path, so the relative URLs inside playlists carry it too.
func (s *Server) play(w http.ResponseWriter, r *http.Request) {
	allowAnyOrigin(w)
	id, token, name := r.PathValue("id"), r.PathValue("token"), r.PathValue("path")
	if !s.app.VerifyPlaybackToken(id, token) {
		http.Error(w, "the playback URL is invalid or expired", http.StatusForbidden)
		return
	}
	if name == "info.json" {
		s.playInfo(w, r, id)
		return
	}
	key := app.PlaybackKey(id, name)
	if key == "" {
		http.NotFound(w, r)
		return
	}
	switch path.Ext(name) {
	case ".ts", ".jpg":
		w.Header().Set("Cache-Control", "public, max-age=86400")
	default:
		w.Header().Set("Cache-Control", "public, max-age=60")
	}
	s.app.Storage().Serve(w, r, key)
}

// playInfo tells the embed page the title of the video and whether it has captions.
func (s *Server) playInfo(w http.ResponseWriter, r *http.Request, id string) {
	video, err := s.app.Store().Video(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) || (err == nil && video.Status != store.VideoReady) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"title":             video.Title,
		"duration":          video.Duration,
		"width":             video.Width,
		"height":            video.Height,
		"captions":          video.CaptionsStatus == store.CaptionsReady,
		"captions_language": video.CaptionsLanguage,
	})
}

// embed serves the player page that sites put in an iframe. The page reads the
// video and the token from its URL, and plays the stream through /play.
func (s *Server) embed(w http.ResponseWriter, r *http.Request) {
	if !s.app.VerifyPlaybackToken(r.PathValue("id"), r.URL.Query().Get("token")) {
		http.Error(w, "The playback URL is invalid or expired.", http.StatusForbidden)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	s.servePage(w, r, "embed.html")
}
