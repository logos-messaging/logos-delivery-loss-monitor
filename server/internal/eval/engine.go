package eval

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/logos-messaging/logos-delivery-loss-monitor/server/internal/apierr"
)

// Clock supplies the evaluation clock in Unix milliseconds.
type Clock interface{ NowMs() int64 }

// SystemClock is the wall clock.
type SystemClock struct{}

// NowMs returns the current Unix time in milliseconds.
func (SystemClock) NowMs() int64 { return time.Now().UnixMilli() }

const (
	queueSize    = 256
	tickInterval = time.Second
	busyRetryMs  = 2000
)

// Engine owns a Core on a single goroutine. HTTP handlers submit commands over a bounded
// channel and wait for the reply, so a 2xx means the command was applied.
type Engine struct {
	core  *Core
	clock Clock
	log   *slog.Logger
	cmds  chan func(now int64)
}

// NewEngine wraps core. Run must be started before commands are submitted.
func NewEngine(core *Core, clock Clock, log *slog.Logger) *Engine {
	return &Engine{core: core, clock: clock, log: log, cmds: make(chan func(int64), queueSize)}
}

// Run processes commands and the 1 s tick until ctx is cancelled.
func (e *Engine) Run(ctx context.Context) {
	t := time.NewTicker(tickInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case f := <-e.cmds:
			f(e.clock.NowMs())
		case <-t.C:
			e.safely(func() { e.core.Tick(e.clock.NowMs()) })
		}
	}
}

func (e *Engine) safely(f func()) (err error) {
	defer func() {
		if r := recover(); r != nil {
			e.log.Error("evaluation panic", "panic", fmt.Sprint(r))
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	f()
	return nil
}

func busy() *apierr.Error {
	return &apierr.Error{Status: http.StatusServiceUnavailable, Code: apierr.Busy, Detail: "server is busy", RetryAfterMs: busyRetryMs}
}

// Do runs f on the evaluation goroutine. It fails with a 503 error when the queue is full.
func (e *Engine) Do(ctx context.Context, f func(c *Core, now int64)) *apierr.Error {
	done := make(chan error, 1)
	cmd := func(now int64) { done <- e.safely(func() { f(e.core, now) }) }
	select {
	case e.cmds <- cmd:
	default:
		return busy()
	}
	select {
	case err := <-done:
		if err != nil {
			return apierr.New(http.StatusInternalServerError, apierr.Internal, "internal error")
		}
		return nil
	case <-ctx.Done():
		return apierr.New(http.StatusServiceUnavailable, apierr.Busy, "request cancelled")
	}
}

// Register submits a registration.
func (e *Engine) Register(ctx context.Context, token, id string, req *RegisterRequest) (res RegisterResult, apiErr *apierr.Error) {
	req.ArrivedMs = e.clock.NowMs()
	if err := e.Do(ctx, func(c *Core, now int64) { res, apiErr = c.Register(now, token, id, req) }); err != nil {
		return RegisterResult{}, err
	}
	return res, apiErr
}

// Ingest submits a records batch.
func (e *Engine) Ingest(ctx context.Context, token, id string, b *Batch) (res IngestResult, apiErr *apierr.Error) {
	b.ArrivedMs = e.clock.NowMs()
	if err := e.Do(ctx, func(c *Core, now int64) { res, apiErr = c.Ingest(now, token, id, b) }); err != nil {
		return IngestResult{}, err
	}
	return res, apiErr
}
