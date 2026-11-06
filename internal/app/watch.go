package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// stalledError says that a program reported no progress for a while, as one that hangs
// on a broken file does.
type stalledError struct {
	what string
	idle time.Duration
}

func (e stalledError) Error() string {
	return fmt.Sprintf("%s made no progress for %s, so deneb stopped it", e.what, e.idle)
}

// progressWatch ends the context of a program when the progress that the program
// reports stops growing for a while, which tells one that hangs from one that works
// slowly.
type progressWatch struct {
	ctx    context.Context
	cancel context.CancelCauseFunc
	idle   time.Duration

	mu       sync.Mutex
	timer    *time.Timer
	fraction float64
}

// watchProgress returns a context for a program, and the watch that ends it when
// advance gets no larger fraction for idle.
func watchProgress(parent context.Context, idle time.Duration, what string) (context.Context, *progressWatch) {
	ctx, cancel := context.WithCancelCause(parent)
	w := &progressWatch{ctx: ctx, cancel: cancel, idle: idle}
	w.timer = time.AfterFunc(idle, func() { cancel(stalledError{what: what, idle: idle}) })
	return ctx, w
}

// advance records the progress of the program, from 0 to 1, which starts the wait
// again when it grew.
func (w *progressWatch) advance(fraction float64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if fraction > w.fraction {
		w.fraction = fraction
		w.timer.Reset(w.idle)
	}
}

// explain returns why the watch stopped the program, in place of err, which only says
// that the program was stopped. Any other err comes back as it is.
func (w *progressWatch) explain(err error) error {
	var stalled stalledError
	if cause := context.Cause(w.ctx); errors.As(cause, &stalled) {
		return stalled
	}
	return err
}

// stop ends the watch and its context.
func (w *progressWatch) stop() {
	w.timer.Stop()
	w.cancel(nil)
}
