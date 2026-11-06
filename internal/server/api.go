package server

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/etak64n/deneb/internal/app"
	"github.com/etak64n/deneb/internal/store"
)

// sessionCookie holds the session token of a signed-in administrator.
const sessionCookie = "deneb_session"

// dashboardHeader marks the requests of the dashboard. A browser sends a custom
// header to another site only after a preflight that deneb never allows, so a page
// on another site cannot make the browser of an administrator change anything.
const dashboardHeader = "X-Deneb-Client"

// caller is who made a request of the API: an administrator or a key.
type caller struct {
	user *store.User
	key  *store.APIKey
}

// api checks the key or the session of a request before its handler runs.
// sessionOnly refuses keys, for what only an administrator may do.
func (s *Server) api(next func(http.ResponseWriter, *http.Request, caller), sessionOnly bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if bearer, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); found {
			if sessionOnly {
				writeError(w, http.StatusForbidden, "keys of the API cannot do this; sign in to the dashboard")
				return
			}
			key, err := s.app.UseAPIKey(r.Context(), strings.TrimSpace(bearer))
			if err != nil {
				if !errors.Is(err, store.ErrNotFound) {
					s.log.Error("check a key of the API", "error", err)
				}
				writeError(w, http.StatusUnauthorized, "the key of the API is wrong or revoked")
				return
			}
			next(w, r, caller{key: &key})
			return
		}
		cookie, err := r.Cookie(sessionCookie)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "send a key of the API as Authorization: Bearer <key>, or sign in")
			return
		}
		user, err := s.app.SessionUser(r.Context(), cookie.Value)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "the session ended; sign in again")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get(dashboardHeader) == "" {
			writeError(w, http.StatusForbidden, "requests of the dashboard need the "+dashboardHeader+" header")
			return
		}
		next(w, r, caller{user: &user})
	}
}

const (
	defaultPageSize = 24
	maxPageSize     = 100
	maxTitleLength  = 300
)

// encodeCursor and decodeCursor keep where a page of videos ends, opaque to clients.
func encodeCursor(video store.Video) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(video.CreatedAt.UnixMilli(), 10) + ":" + video.ID))
}

func decodeCursor(value string) (*store.VideoCursor, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, false
	}
	millis, id, found := strings.Cut(string(raw), ":")
	created, err := strconv.ParseInt(millis, 10, 64)
	if !found || err != nil || id == "" {
		return nil, false
	}
	return &store.VideoCursor{CreatedAt: time.UnixMilli(created).UTC(), ID: id}, true
}

func (s *Server) listVideos(w http.ResponseWriter, r *http.Request, _ caller) {
	limit := defaultPageSize
	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > maxPageSize {
			writeError(w, http.StatusBadRequest, "limit must be a whole number from 1 to 100")
			return
		}
		limit = value
	}
	var after *store.VideoCursor
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		cursor, ok := decodeCursor(raw)
		if !ok {
			writeError(w, http.StatusBadRequest, "cursor is not one that deneb gave")
			return
		}
		after = cursor
	}
	// One more than the page tells whether another page follows.
	videos, err := s.app.Store().ListVideos(r.Context(), after, limit+1)
	if err != nil {
		s.fail(w, err)
		return
	}
	next := ""
	if len(videos) > limit {
		videos = videos[:limit]
		next = encodeCursor(videos[limit-1])
	}
	base := s.baseURL(r)
	views := make([]app.VideoView, 0, len(videos))
	for _, video := range videos {
		views = append(views, s.app.View(video, base))
	}
	writeJSON(w, http.StatusOK, map[string]any{"videos": views, "next_cursor": next})
}

func (s *Server) createVideo(w http.ResponseWriter, r *http.Request, _ caller) {
	var body struct {
		Title string `json:"title"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	title := strings.TrimSpace(body.Title)
	if utf8.RuneCountInString(title) > maxTitleLength {
		writeError(w, http.StatusBadRequest, "the title can be at most 300 characters")
		return
	}
	base := s.baseURL(r)
	video, upload, err := s.app.CreateVideo(r.Context(), title, base)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"video": s.app.View(video, base), "upload": upload})
}

func (s *Server) getVideo(w http.ResponseWriter, r *http.Request, _ caller) {
	video, err := s.app.Store().Video(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"video": s.app.View(video, s.baseURL(r))})
}

func (s *Server) updateVideo(w http.ResponseWriter, r *http.Request, _ caller) {
	var body struct {
		Title *string `json:"title"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	id := r.PathValue("id")
	if body.Title != nil {
		title := strings.TrimSpace(*body.Title)
		if utf8.RuneCountInString(title) > maxTitleLength {
			writeError(w, http.StatusBadRequest, "the title can be at most 300 characters")
			return
		}
		if err := s.app.Store().SetVideoTitle(r.Context(), id, title, s.app.Now()); err != nil {
			s.fail(w, err)
			return
		}
	}
	s.getVideo(w, r, caller{})
}

func (s *Server) deleteVideo(w http.ResponseWriter, r *http.Request, _ caller) {
	if err := s.app.DeleteVideo(r.Context(), r.PathValue("id")); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// webhookView shows the secret only when the webhook is created.
type webhookView struct {
	ID        string    `json:"id"`
	URL       string    `json:"url"`
	Secret    string    `json:"secret,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Server) listWebhooks(w http.ResponseWriter, r *http.Request, _ caller) {
	hooks, err := s.app.Store().ListWebhooks(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	views := make([]webhookView, 0, len(hooks))
	for _, hook := range hooks {
		views = append(views, webhookView{ID: hook.ID, URL: hook.URL, CreatedAt: hook.CreatedAt})
	}
	writeJSON(w, http.StatusOK, map[string]any{"webhooks": views})
}

func (s *Server) createWebhook(w http.ResponseWriter, r *http.Request, _ caller) {
	var body struct {
		URL string `json:"url"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	hook, err := s.app.CreateWebhook(r.Context(), body.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"webhook": webhookView{ID: hook.ID, URL: hook.URL, Secret: hook.Secret, CreatedAt: hook.CreatedAt}})
}

func (s *Server) deleteWebhook(w http.ResponseWriter, r *http.Request, _ caller) {
	if err := s.app.Store().DeleteWebhook(r.Context(), r.PathValue("id")); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listDeliveries(w http.ResponseWriter, r *http.Request, _ caller) {
	found, err := s.app.Store().WebhookExists(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "the webhook does not exist")
		return
	}
	deliveries, err := s.app.Store().ListDeliveries(r.Context(), r.PathValue("id"), 50)
	if err != nil {
		s.fail(w, err)
		return
	}
	type deliveryView struct {
		ID             string     `json:"id"`
		Event          string     `json:"event"`
		Status         string     `json:"status"`
		Attempts       int        `json:"attempts"`
		ResponseStatus int        `json:"response_status"`
		LastError      string     `json:"last_error,omitempty"`
		CreatedAt      time.Time  `json:"created_at"`
		DeliveredAt    *time.Time `json:"delivered_at"`
	}
	views := make([]deliveryView, 0, len(deliveries))
	for _, d := range deliveries {
		views = append(views, deliveryView{d.ID, d.Event, d.Status, d.Attempts, d.ResponseStatus, d.LastError, d.CreatedAt, d.DeliveredAt})
	}
	writeJSON(w, http.StatusOK, map[string]any{"deliveries": views})
}

// apiKeyView shows the secret only when the key is created.
type apiKeyView struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Secret     string     `json:"secret,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

func (s *Server) listAPIKeys(w http.ResponseWriter, r *http.Request, _ caller) {
	keys, err := s.app.Store().ListAPIKeys(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	views := make([]apiKeyView, 0, len(keys))
	for _, key := range keys {
		views = append(views, apiKeyView{ID: key.ID, Name: key.Name, Prefix: key.Prefix, CreatedAt: key.CreatedAt, LastUsedAt: key.LastUsedAt})
	}
	writeJSON(w, http.StatusOK, map[string]any{"api_keys": views})
}

func (s *Server) createAPIKey(w http.ResponseWriter, r *http.Request, _ caller) {
	var body struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	key, secret, err := s.app.CreateAPIKey(r.Context(), body.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"api_key": apiKeyView{ID: key.ID, Name: key.Name, Prefix: key.Prefix, Secret: secret, CreatedAt: key.CreatedAt}})
}

func (s *Server) deleteAPIKey(w http.ResponseWriter, r *http.Request, _ caller) {
	if err := s.app.Store().DeleteAPIKey(r.Context(), r.PathValue("id")); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// fail answers an error: 404 for a missing row, and 500 with a log for the rest.
func (s *Server) fail(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	s.log.Error("answer a request", "error", err)
	writeError(w, http.StatusInternalServerError, "something went wrong on the server")
}
