//go:build linux || darwin

package app

import "syscall"

// freeSpace returns the bytes that deneb may still write on the file system of dir,
// and false when it cannot tell.
func freeSpace(dir string) (uint64, bool) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(dir, &stat); err != nil {
		return 0, false
	}
	return stat.Bavail * uint64(stat.Bsize), true
}
