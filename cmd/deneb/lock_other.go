//go:build !unix

package main

import "os"

// lockDataDir does nothing on systems without flock, where deneb does not run in production.
func lockDataDir(string) (*os.File, error) {
	return os.Open(os.DevNull)
}
