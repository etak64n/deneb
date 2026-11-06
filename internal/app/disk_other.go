//go:build !linux && !darwin

package app

// freeSpace cannot tell the free space where deneb does not read it, which skips the checks.
func freeSpace(string) (uint64, bool) { return 0, false }
