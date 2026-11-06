package media

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
)

// tail keeps the last bytes that a program writes, which hold its error message.
type tail struct {
	mu   sync.Mutex
	data []byte
	max  int
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.data = append(t.data, p...)
	if len(t.data) > t.max {
		t.data = t.data[len(t.data)-t.max:]
	}
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.data)
}

// run starts a program and waits for it. Each line of its standard output goes to
// onLine, when given. A failure carries the end of the standard error.
func run(ctx context.Context, name string, args []string, onLine func(string)) error {
	cmd := exec.CommandContext(ctx, name, args...)
	errors := &tail{max: 4096}
	cmd.Stderr = errors
	var stdout io.ReadCloser
	if onLine != nil {
		pipe, err := cmd.StdoutPipe()
		if err != nil {
			return err
		}
		stdout = pipe
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", name, err)
	}
	if stdout != nil {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			onLine(scanner.Text())
		}
		// Read whatever the scanner left, such as after a line too long for it, so that
		// the program never blocks on a full pipe.
		io.Copy(io.Discard, stdout)
	}
	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%s failed: %s", name, lastLines(errors.String(), 4, err))
	}
	return nil
}

// progressReader turns the -progress output of ffmpeg into a fraction of the duration.
func progressReader(duration float64, report func(float64)) func(string) {
	return func(line string) {
		if report == nil || duration <= 0 {
			return
		}
		value, found := strings.CutPrefix(line, "out_time_us=")
		if !found {
			return
		}
		var micros float64
		if _, err := fmt.Sscan(value, &micros); err != nil || micros < 0 {
			return
		}
		report(min(1, micros/1e6/duration))
	}
}
