package server

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"

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

// play serves the files of a ready video under a signed URL, or under "permanent" in
// place of the token for permanent URLs: /play/<video>/<token>/hls/master.m3u8, its
// playlists and segments, poster.jpg, storyboard.vtt and storyboard.jpg, captions.vtt,
// and info.json for the embed page. The token sits in the path, so the relative URLs
// inside playlists carry it too.
func (s *Server) play(w http.ResponseWriter, r *http.Request) {
	allowAnyOrigin(w)
	id, token, name := r.PathValue("id"), r.PathValue("token"), r.PathValue("path")
	allowed, err := s.app.PlaybackAllowed(r.Context(), id, token)
	if err != nil {
		s.fail(w, err)
		return
	}
	if !allowed {
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
	if name == "storyboard.vtt" {
		s.storyboard(w, r, id, token, key)
		return
	}
	cache := "public, max-age=60"
	if ext := path.Ext(name); ext == ".ts" || ext == ".jpg" {
		cache = "public, max-age=86400"
	}
	s.app.Storage().Serve(&cacheOnSuccess{ResponseWriter: w, cache: cache}, r, key)
}

// cacheOnSuccess lets browsers and CDNs keep the files that deneb serves, and nothing
// else: an error, or a redirect to a signed URL of the bucket that expires in minutes.
type cacheOnSuccess struct {
	http.ResponseWriter
	cache   string
	written bool
}

func (c *cacheOnSuccess) WriteHeader(status int) {
	if !c.written {
		c.written = true
		if status == http.StatusOK || status == http.StatusPartialContent || status == http.StatusNotModified {
			c.Header().Set("Cache-Control", c.cache)
		} else {
			c.Header().Set("Cache-Control", "no-store")
		}
	}
	c.ResponseWriter.WriteHeader(status)
}

func (c *cacheOnSuccess) Write(data []byte) (int, error) {
	if !c.written {
		c.WriteHeader(http.StatusOK)
	}
	return c.ResponseWriter.Write(data)
}

func (c *cacheOnSuccess) Unwrap() http.ResponseWriter { return c.ResponseWriter }

// storyboard serves the storyboard with the URL of its picture made absolute. Players
// resolve a relative URL in it against the page that plays the video, rather than
// against the storyboard, which would point the embed page and the dashboard elsewhere.
func (s *Server) storyboard(w http.ResponseWriter, r *http.Request, id, token, key string) {
	data, err := s.app.Storage().ReadFile(r.Context(), key)
	if errors.Is(err, fs.ErrNotExist) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.log.Error("read a storyboard", "video", id, "error", err)
		http.Error(w, "could not reach the storage", http.StatusBadGateway)
		return
	}
	sheet := fmt.Sprintf("%s/play/%s/%s/storyboard.jpg#", s.baseURL(r), id, token)
	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=60")
	io.WriteString(w, strings.ReplaceAll(string(data), "\nstoryboard.jpg#", "\n"+sheet))
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
// video and the token from its URL, and plays the stream through /play; a permanent
// embed URL has no token. With a token that is wrong or expired, or a video whose URLs
// are not permanent, the page still loads, with 403, and says the video is not available,
// rather than a bare error in the frame of another site.
func (s *Server) embed(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		token = app.PermanentToken
	}
	status := http.StatusOK
	allowed, err := s.app.PlaybackAllowed(r.Context(), r.PathValue("id"), token)
	if err != nil {
		s.log.Error("check an embed URL", "video", r.PathValue("id"), "error", err)
		status = http.StatusInternalServerError
	} else if !allowed {
		status = http.StatusForbidden
	}
	w.Header().Set("Cache-Control", "no-cache")
	s.servePage(w, r, "embed.html", status)
}
