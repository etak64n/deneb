//go:build unix

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// lockDataDir keeps a second deneb from using the same data folder, where both would
// take the same jobs and remove the work of the other. The lock ends with the process.
func lockDataDir(dir string) (*os.File, error) {
	file, err := os.OpenFile(filepath.Join(dir, "deneb.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("lock the data folder: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("another deneb is using the data folder %s", dir)
		}
		return nil, fmt.Errorf("lock the data folder: %w", err)
	}
	return file, nil
}
