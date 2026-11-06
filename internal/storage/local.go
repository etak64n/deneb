package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Local keeps the files under a folder, normally on the volume of the container.
type Local struct {
	root string
}

// NewLocal returns a storage in the folder, which it creates if needed.
func NewLocal(root string) (*Local, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &Local{root: root}, nil
}

// path turns a key into a path under the root. Keys come from deneb itself, but a
// key that leaves the root is refused all the same.
func (l *Local) path(key string) (string, error) {
	cleaned := filepath.Clean(filepath.FromSlash(key))
	if cleaned == "." || strings.HasPrefix(cleaned, "..") || filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("invalid storage key %q", key)
	}
	return filepath.Join(l.root, cleaned), nil
}

func (l *Local) MoveFile(ctx context.Context, key, localPath string) error {
	target, err := l.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if err := os.Rename(localPath, target); err == nil {
		return nil
	}
	// The file is on another file system, such as a different volume: copy it.
	if err := copyFile(localPath, target); err != nil {
		return err
	}
	return os.Remove(localPath)
}

func (l *Local) PutFile(ctx context.Context, key, localPath string) error {
	target, err := l.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return copyFile(localPath, target)
}

func (l *Local) LocalFile(ctx context.Context, key, dir string) (string, func(), error) {
	target, err := l.path(key)
	if err != nil {
		return "", nil, err
	}
	if _, err := os.Stat(target); err != nil {
		return "", nil, err
	}
	return target, func() {}, nil
}

func (l *Local) Serve(w http.ResponseWriter, r *http.Request, key string) {
	target, err := l.path(key)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	file, err := os.Open(target)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "could not read the file", http.StatusInternalServerError)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		http.Error(w, "could not read the file", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", ContentType(key))
	// ServeContent answers ranges and conditional requests.
	http.ServeContent(w, r, "", info.ModTime(), file)
}

func (l *Local) DeletePrefix(ctx context.Context, prefix string) error {
	target, err := l.path(strings.TrimSuffix(prefix, "/"))
	if err != nil {
		return err
	}
	return os.RemoveAll(target)
}

// copyFile copies a file through a temporary name, so that a reader never sees half of it.
func copyFile(from, to string) error {
	source, err := os.Open(from)
	if err != nil {
		return err
	}
	defer source.Close()
	temporary := to + ".part"
	target, err := os.Create(temporary)
	if err != nil {
		return err
	}
	if _, err := io.Copy(target, source); err != nil {
		target.Close()
		os.Remove(temporary)
		return err
	}
	if err := target.Close(); err != nil {
		os.Remove(temporary)
		return err
	}
	return os.Rename(temporary, to)
}
