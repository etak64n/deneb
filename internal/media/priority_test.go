//go:build linux || darwin

package media

import (
	"context"
	"strings"
	"testing"
)

func TestProgramsRunAtALowerPriority(t *testing.T) {
	var lines []string
	// The shell waits before it asks for its niceness, which deneb sets just after the start.
	err := run(context.Background(), "sh", []string{"-c", "sleep 0.2; nice"}, func(line string) { lines = append(lines, line) })
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(lines, "\n"); got != "10" {
		t.Errorf("niceness %q, want 10", got)
	}
}
