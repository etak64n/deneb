// Package server answers the HTTP requests of deneb: the API, the tus uploads, the
// signed playback of videos, the embed page, and the dashboard.
package server

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/etak64n/deneb/internal/app"
	"github.com/tus/tusd/v2/pkg/handler"
	"github.com/tus/tusd/v2/pkg/memorylocker"
)

// Server is the HTTP side of deneb.
type Server struct {
	app *app.App
	log *slog.Logger
	ui  fs.FS
	tus *handler.Handler
	// locker lets one request at a time write the upload of a video: each request of
	// tus takes it, and so does a PUT.
	locker  *memorylocker.MemoryLocker
	mux     *http.ServeMux
	limiter *limiter
	// gzipped keeps the files of ui compressed by gzipFile, by their names.
	gzipped sync.Map
}

// New returns the server. ui holds the built dashboard, with index.html and embed.html at its root.
func New(a *app.App, ui fs.FS, log *slog.Logger) (*Server, error) {
	s := &Server{app: a, log: log, ui: ui, locker: memorylocker.New(), mux: http.NewServeMux(), limiter: newLimiter(10, 10*time.Minute)}
	tus, err := s.newTus()
	if err != nil {
		return nil, err
	}
	s.tus = tus
	s.routes()
	return s, nil
}

// Handler returns the handler of every request.
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		s.mux.ServeHTTP(w, r)
	})
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	s.mux.HandleFunc("GET /auth/state", s.authState)
	s.mux.HandleFunc("POST /auth/setup", s.setup)
	s.mux.HandleFunc("POST /auth/login", s.login)
	s.mux.HandleFunc("POST /auth/logout", s.logout)

	uploads := s.uploadURLs(s.tus)
	s.mux.Handle("/v1/uploads/", s.guardUploads(http.StripPrefix("/v1/uploads/", uploads)))
	s.mux.Handle("/v1/uploads", s.guardUploads(http.StripPrefix("/v1/uploads", uploads)))

	s.mux.HandleFunc("GET /v1/videos", s.api(s.listVideos, false))
	s.mux.HandleFunc("POST /v1/videos", s.api(s.createVideo, false))
	s.mux.HandleFunc("GET /v1/videos/{id}", s.api(s.getVideo, false))
	s.mux.HandleFunc("PATCH /v1/videos/{id}", s.api(s.updateVideo, false))
	s.mux.HandleFunc("DELETE /v1/videos/{id}", s.api(s.deleteVideo, false))
	s.mux.HandleFunc("POST /v1/videos/{id}/retry", s.api(s.retryVideo, false))

	s.mux.HandleFunc("GET /v1/webhooks", s.api(s.listWebhooks, false))
	s.mux.HandleFunc("POST /v1/webhooks", s.api(s.createWebhook, false))
	s.mux.HandleFunc("DELETE /v1/webhooks/{id}", s.api(s.deleteWebhook, false))
	s.mux.HandleFunc("GET /v1/webhooks/{id}/deliveries", s.api(s.listDeliveries, false))

	// A key of the API cannot make or revoke keys: only a signed-in administrator can.
	s.mux.HandleFunc("GET /v1/api-keys", s.api(s.listAPIKeys, true))
	s.mux.HandleFunc("POST /v1/api-keys", s.api(s.createAPIKey, true))
	s.mux.HandleFunc("DELETE /v1/api-keys/{id}", s.api(s.deleteAPIKey, true))

	s.mux.HandleFunc("GET /play/{id}/{token}/{path...}", s.play)
	s.mux.HandleFunc("OPTIONS /play/{id}/{token}/{path...}", s.playOptions)
	s.mux.HandleFunc("GET /embed/{id}", s.embed)

	s.mux.HandleFunc("/v1/", func(w http.ResponseWriter, r *http.Request) { writeError(w, http.StatusNotFound, "no such endpoint") })
	// "/" rather than "GET /": a pattern with a method would conflict with "/v1/uploads/", which has none.
	s.mux.HandleFunc("/", s.dashboard)
}

// baseURL is the start of the URLs that deneb hands out: DENEB_PUBLIC_URL, or the
// scheme and host that the request came to.
func (s *Server) baseURL(r *http.Request) string {
	if public := s.app.Config().PublicURL; public != "" {
		return public
	}
	scheme := "http"
	if s.secure(r) {
		scheme = "https"
	}
	host := r.Host
	if forwarded := r.Header.Get("X-Forwarded-Host"); forwarded != "" && s.app.Config().TrustProxy {
		host = strings.TrimSpace(strings.Split(forwarded, ",")[0])
	}
	return scheme + "://" + host
}

// secure tells whether the browser reaches deneb over HTTPS.
func (s *Server) secure(r *http.Request) bool {
	if public := s.app.Config().PublicURL; public != "" {
		return strings.HasPrefix(public, "https://")
	}
	return r.TLS != nil || (s.app.Config().TrustProxy && r.Header.Get("X-Forwarded-Proto") == "https")
}

// clientIP returns the address of the client. Behind a trusted proxy, it is the last
// address in X-Forwarded-For, which the proxy added; the client may forge the others.
func (s *Server) clientIP(r *http.Request) string {
	if lines := r.Header.Values("X-Forwarded-For"); len(lines) > 0 && s.app.Config().TrustProxy {
		// The proxy adds the address it saw last, after whatever the client wrote,
		// whether to the last line or as a line of its own.
		addresses := strings.Split(lines[len(lines)-1], ",")
		return strings.TrimSpace(addresses[len(addresses)-1])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// limitKey is what the limit of sign-in attempts counts by: an IPv4 address, or the
// /64 network of an IPv6 address, of which anyone has more addresses than they can use.
func limitKey(address string) string {
	ip := net.ParseIP(address)
	if ip == nil || ip.To4() != nil {
		return address
	}
	return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
}

// uploadURLs makes tusd build the URLs of uploads from baseURL, as every other URL
// of deneb is. tusd reads the scheme and host from these headers, which replace any
// that the client sent.
func (s *Server) uploadURLs(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scheme, host, _ := strings.Cut(s.baseURL(r), "://")
		r.Header.Del("Forwarded")
		r.Header.Set("X-Forwarded-Proto", scheme)
		r.Header.Set("X-Forwarded-Host", host)
		next.ServeHTTP(w, r)
	})
}

// errorBody is the form of every error of the API.
type errorBody struct {
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	var body errorBody
	body.Error.Message = message
	writeJSON(w, status, body)
}

// readJSON decodes a JSON body of at most 1 MB into value.
func readJSON(w http.ResponseWriter, r *http.Request, value any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil && !errors.Is(err, io.EOF) {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "the body is larger than 1 MB")
			return false
		}
		writeError(w, http.StatusBadRequest, "the body is not valid JSON: "+err.Error())
		return false
	}
	return true
}

// dashboard serves the files of the dashboard, and its page for every other path,
// where the dashboard's own router takes over.
func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name != "" && name != "index.html" && name != "embed.html" {
		if info, err := fs.Stat(s.ui, name); err == nil && !info.IsDir() {
			if strings.HasPrefix(name, "assets/") {
				// Vite names the assets by their content, so they never change.
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			s.serveFile(w, r, name)
			return
		}
		// An asset of another version, as asked by a tab opened before an upgrade, is
		// missing rather than the dashboard, which the browser cannot run as a script.
		if strings.HasPrefix(name, "assets/") {
			http.NotFound(w, r)
			return
		}
	}
	// The dashboard is never framed by another site.
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Cache-Control", "no-cache")
	s.servePage(w, r, "index.html", http.StatusOK)
}

func (s *Server) servePage(w http.ResponseWriter, r *http.Request, name string, status int) {
	page, err := fs.ReadFile(s.ui, name)
	if err != nil {
		http.Error(w, "The dashboard is not built. Run `pnpm --dir web build` before `go build`.", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Add("Vary", "Accept-Encoding")
	if acceptsGzip(r) {
		if compressed, err := s.gzipFile(name); err == nil {
			w.Header().Set("Content-Encoding", "gzip")
			page = compressed
		}
	}
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		w.Write(page)
	}
}

// serveFile answers a file of the dashboard, compressed with gzip when the browser
// takes it and the file is of a kind that gzip shrinks. The embed page loads a
// megabyte of scripts, a third of that once compressed.
func (s *Server) serveFile(w http.ResponseWriter, r *http.Request, name string) {
	w.Header().Add("Vary", "Accept-Encoding")
	// A range is a range of the file as it is, which a compressed answer would not keep.
	if compressible(name) && acceptsGzip(r) && r.Header.Get("Range") == "" {
		if compressed, err := s.gzipFile(name); err == nil {
			w.Header().Set("Content-Type", mime.TypeByExtension(path.Ext(name)))
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Set("Content-Length", strconv.Itoa(len(compressed)))
			if r.Method != http.MethodHead {
				w.Write(compressed)
			}
			return
		}
	}
	http.ServeFileFS(w, r, s.ui, name)
}

// compressible tells whether gzip shrinks a file of the dashboard: scripts, styles,
// pages, and SVG. Pictures in other formats and WOFF2 fonts are compressed already.
func compressible(name string) bool {
	switch path.Ext(name) {
	case ".js", ".css", ".html", ".svg", ".json", ".map", ".txt":
		return true
	}
	return false
}

// gzipFile returns a file of the dashboard compressed with gzip. It compresses each
// file once, since the files do not change while deneb runs.
func (s *Server) gzipFile(name string) ([]byte, error) {
	if kept, found := s.gzipped.Load(name); found {
		return kept.([]byte), nil
	}
	data, err := fs.ReadFile(s.ui, name)
	if err != nil {
		return nil, err
	}
	var buffer bytes.Buffer
	writer, err := gzip.NewWriterLevel(&buffer, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err := writer.Write(data); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	s.gzipped.Store(name, buffer.Bytes())
	return buffer.Bytes(), nil
}

// acceptsGzip tells whether the browser takes answers compressed with gzip, by its
// Accept-Encoding header, where gzip;q=0 refuses them.
func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		coding, params, _ := strings.Cut(part, ";")
		if !strings.EqualFold(strings.TrimSpace(coding), "gzip") {
			continue
		}
		for _, param := range strings.Split(params, ";") {
			name, value, _ := strings.Cut(strings.TrimSpace(param), "=")
			if strings.EqualFold(name, "q") {
				if q, err := strconv.ParseFloat(value, 64); err == nil && q == 0 {
					return false
				}
			}
		}
		return true
	}
	return false
}

// limiter allows a number of attempts per client within a window, against password guessing.
type limiter struct {
	mu       sync.Mutex
	max      int
	window   time.Duration
	attempts map[string][]time.Time
}

func newLimiter(max int, window time.Duration) *limiter {
	return &limiter{max: max, window: window, attempts: map[string][]time.Time{}}
}

// allow records an attempt and tells whether the client is still within the limit.
func (l *limiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	recent := l.attempts[key][:0]
	for _, at := range l.attempts[key] {
		if now.Sub(at) < l.window {
			recent = append(recent, at)
		}
	}
	if len(recent) >= l.max {
		l.attempts[key] = recent
		return false
	}
	l.attempts[key] = append(recent, now)
	if len(l.attempts) > 10_000 {
		// Forget everyone rather than grow without end under a flood of addresses.
		l.attempts = map[string][]time.Time{key: l.attempts[key]}
	}
	return true
}
