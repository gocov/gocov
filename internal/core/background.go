// Work the pipeline goes on with after it has answered a request: the
// follow-up forge pushes for uploads that queued behind another one
// (publish.go) and the visibility re-checks a stale page load starts
// (repo.go). It all runs under one context and one wait group, so a
// stopping server can let it finish and then cut it short in one place.

package core

import (
	"context"
	"errors"
	"sync"
	"time"
)

// background tracks the pipeline's detached goroutines. The zero value is
// ready to use.
type background struct {
	once sync.Once
	ctx  context.Context // cancelled by drain to cut the stragglers short
	stop context.CancelFunc
	wg   sync.WaitGroup
}

func (b *background) init() {
	b.once.Do(func() { b.ctx, b.stop = context.WithCancel(context.Background()) })
}

// Go runs fn in the background under the pipeline's lifetime context, not
// a request's: the work outlives the request that started it.
func (b *background) Go(fn func(ctx context.Context)) {
	b.init()
	b.wg.Go(func() { fn(b.ctx) })
}

var (
	errBackgroundCut       = errors.New("background work cut short; a commit whose forge push was cut is pushed with its next upload")
	errBackgroundAbandoned = errors.New("background work did not stop in time")
)

// drain waits for the background work until unwind before ctx's
// deadline, then cancels it and waits for it to unwind — release its
// leases — never past the deadline. Without a deadline it waits until ctx
// is done.
func (b *background) drain(ctx context.Context, unwind time.Duration) error {
	b.init()
	idle := make(chan struct{})
	go func() { b.wg.Wait(); close(idle) }()
	cut := ctx
	if d, ok := ctx.Deadline(); ok {
		var cancel context.CancelFunc
		cut, cancel = context.WithDeadline(ctx, d.Add(-unwind))
		defer cancel()
	}
	select {
	case <-idle:
		return nil
	case <-cut.Done():
	}
	b.stop()
	select {
	case <-idle:
		return errBackgroundCut
	case <-ctx.Done():
		return errBackgroundAbandoned
	}
}

// Drain lets the pipeline's background work finish until shortly before
// ctx's deadline, then cuts it short, leaving it the time it needs to
// release what it holds. A server shutting down calls it after its last
// request is done and before it closes the store, which that work still
// needs. Cut work loses nothing: a forge push not finished is the
// commit's next upload's to make, and a visibility re-check is asked again.
func (p *Pipeline) Drain(ctx context.Context) error {
	return p.bg.drain(ctx, finishPushTimeout+time.Second)
}
