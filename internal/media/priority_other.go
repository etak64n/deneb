//go:build !linux && !darwin

package media

// lowerPriority leaves a program as it is where deneb does not set niceness.
func lowerPriority(int) {}
