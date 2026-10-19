package eval

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/logos-messaging/logos-delivery-loss-monitor/server/internal/config"
	"github.com/logos-messaging/logos-delivery-loss-monitor/server/internal/metrics"
)

type fixedClock int64

func (f fixedClock) NowMs() int64 { return int64(f) }

func newEngine(t *testing.T) *Engine {
	t.Helper()
	env := map[string]string{"LOSSMON_TOKENS_FILE": "x", "LOSSMON_PROBE_KEY_FILE": "y"}
	cfg, err := config.Load(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	m := metrics.New(cfg.Topics, cfg.Locations, cfg.LossRatioWindow)
	return NewEngine(NewCore(cfg, m, log, keyID, t0), fixedClock(t0), log)
}

func TestEngineAnswers503WhenQueueIsFull(t *testing.T) {
	e := newEngine(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for i := 0; i < queueSize; i++ {
		go e.Do(ctx, func(*Core, int64) {})
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(e.cmds) < queueSize {
		if time.Now().After(deadline) {
			t.Fatalf("queue never filled: %d", len(e.cmds))
		}
		time.Sleep(time.Millisecond)
	}
	err := e.Do(ctx, func(*Core, int64) {})
	if err == nil || err.Status != 503 || err.RetryAfterMs != 2000 {
		t.Fatalf("want 503 with retry hint, got %+v", err)
	}
}

func TestEngineRecoversFromPanics(t *testing.T) {
	e := newEngine(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go e.Run(ctx)
	if err := e.Do(ctx, func(*Core, int64) { panic("boom") }); err == nil || err.Status != 500 {
		t.Fatalf("want 500, got %+v", err)
	}
	if err := e.Do(ctx, func(*Core, int64) {}); err != nil {
		t.Fatalf("engine did not survive the panic: %v", err)
	}
}
