// deneb is a self-hosted video API: it takes uploads, encodes them into HLS in
// several qualities with ffmpeg, and serves them under signed URLs, with an
// embeddable player, webhooks, and a dashboard. One container runs all of it.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/etak64n/deneb/internal/app"
	"github.com/etak64n/deneb/internal/config"
	"github.com/etak64n/deneb/internal/server"
	"github.com/etak64n/deneb/internal/storage"
	"github.com/etak64n/deneb/internal/store"
	"github.com/etak64n/deneb/web"
)

// version is set at build time with -ldflags "-X main.version=v0.1.0".
var version = "dev"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version":
			fmt.Println(version)
			return
		case "healthcheck":
			os.Exit(healthcheck())
		default:
			fmt.Fprintf(os.Stderr, "usage: deneb [version | healthcheck]\n")
			os.Exit(2)
		}
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("deneb stopped", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	app.Version = version
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return fmt.Errorf("create the data folder: %w", err)
	}
	lock, err := lockDataDir(cfg.DataDir)
	if err != nil {
		return err
	}
	defer lock.Close()
	db, err := store.Open(ctx, filepath.Join(cfg.DataDir, "deneb.db"))
	if err != nil {
		return err
	}
	defer db.Close()

	var files storage.Storage
	switch cfg.Storage {
	case config.StorageS3:
		bucket, err := storage.NewS3(ctx, cfg.S3)
		if err != nil {
			return err
		}
		if err := bucket.Check(ctx); err != nil {
			return err
		}
		files = bucket
	default:
		local, err := storage.NewLocal(filepath.Join(cfg.DataDir, "media"))
		if err != nil {
			return err
		}
		files = local
	}

	deneb, err := app.New(ctx, cfg, db, files, log)
	if err != nil {
		return err
	}
	srv, err := server.New(deneb, web.FS(), log)
	if err != nil {
		return err
	}
	httpServer := &http.Server{
		Addr:              cfg.Listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	done := make(chan struct{})
	go func() { deneb.Run(ctx); close(done) }()
	serveErr := make(chan error, 1)
	go func() { serveErr <- httpServer.ListenAndServe() }()
	log.Info("deneb is running", "version", version, "listen", cfg.Listen, "storage", cfg.Storage,
		"workers", cfg.Workers, "captions", cfg.Captions.Enabled)
	if code := deneb.SetupCode(); code != "" {
		log.Info("deneb has no administrator yet; create one in the dashboard with this setup code", "setup_code", code)
	}

	select {
	case err := <-serveErr:
		stop()
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
	}
	log.Info("deneb is stopping")
	shutdown, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	httpServer.Shutdown(shutdown)
	<-done
	return nil
}

// healthcheck asks the running server whether it answers, for HEALTHCHECK of Docker.
func healthcheck() int {
	listen := os.Getenv("DENEB_LISTEN")
	if listen == "" {
		listen = ":8080"
	}
	if strings.HasPrefix(listen, ":") {
		listen = "127.0.0.1" + listen
	}
	client := http.Client{Timeout: 3 * time.Second}
	response, err := client.Get("http://" + listen + "/healthz")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, response.Status)
		return 1
	}
	return 0
}
