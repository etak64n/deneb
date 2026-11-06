//go:build linux || darwin

package media

import "syscall"

// background is the niceness of the programs that deneb runs: higher than the 0 of
// deneb itself, so that they yield the CPU to it.
const background = 10

// lowerPriority makes a program that just started yield the CPU to deneb, which
// answers the API and serves playback while the program encodes or transcribes. The
// threads that the program starts later inherit the niceness. With the CPU idle, the
// program still takes all of it. A program that cannot be lowered runs as it is.
func lowerPriority(pid int) {
	syscall.Setpriority(syscall.PRIO_PROCESS, pid, background)
}
