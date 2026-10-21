// Command lossmon-server collects publish and receive reports from lossmon clients and
// exports message-loss metrics for a Logos Delivery network.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/logos-messaging/logos-delivery-loss-monitor/server/internal/api"
	"github.com/logos-messaging/logos-delivery-loss-monitor/server/internal/auth"
	"github.com/logos-messaging/logos-delivery-loss-monitor/server/internal/config"
	"github.com/logos-messaging/logos-delivery-loss-monitor/server/internal/eval"
	"github.com/logos-messaging/logos-delivery-loss-monitor/server/internal/landing"
	"github.com/logos-messaging/logos-delivery-loss-monitor/server/internal/metrics"
)

const exitConfig = 2

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if len(os.Args) == 2 && (os.Args[1] == "--version" || os.Args[1] == "-version") {
		fmt.Println("lossmon-server " + version)
		return
	}
	os.Exit(run())
}

func run() int {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		return exitConfig
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(cfg.LogLevel)); err != nil {
		level = slog.LevelInfo
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	key, err := config.LoadProbeKey(cfg.ProbeKeyFile)
	if err != nil {
		log.Error("invalid configuration", "error", err)
		return exitConfig
	}
	tokens := &auth.Store{}
	if err := tokens.Load(cfg.TokensFile); err != nil {
		log.Error("invalid configuration", "error", err)
		return exitConfig
	}

	m := metrics.New(cfg.Topics, cfg.Locations, cfg.LossRatioWindow)
	clock := eval.SystemClock{}
	core := eval.NewCore(cfg, m, log, key.ID, clock.NowMs())
	engine := eval.NewEngine(core, clock, log)
	srv := api.New(cfg, tokens, engine, m, key, clock.NowMs, log)

	landingPage, err := landing.Render(cfg, version)
	if err != nil {
		log.Error("landing page render failed", "error", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go engine.Run(ctx)
	go reloadOnSIGHUP(ctx, tokens, cfg.TokensFile, log)

	apiSrv := srv.HTTPServer(cfg.Listen, landing.Mount(srv.Handler(), landingPage))
	metricsSrv := srv.HTTPServer(cfg.MetricsListen, srv.MetricsHandler())
	errc := make(chan error, 2)
	for _, s := range []*http.Server{apiSrv, metricsSrv} {
		go func() { errc <- s.ListenAndServe() }()
	}
	log.Info("lossmon-server started", "version", version, "api", cfg.Listen, "metrics", cfg.MetricsListen, "preset", cfg.Preset, "public_url", cfg.PublicURL, "probe_key_id", key.ID)
	if cfg.PublicURL == "" {
		log.Warn("landing page shows a placeholder server URL: LOSSMON_PUBLIC_URL is unset")
	}
	if !landing.IsRelease(version) {
		log.Warn("landing page has no install line: server version is not a release", "version", version)
	}

	code := 0
	select {
	case <-ctx.Done():
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Error("listener failed", "error", err)
			code = 1
		}
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, s := range []*http.Server{apiSrv, metricsSrv} {
		_ = s.Shutdown(shutdown)
	}
	return code
}

// reloadOnSIGHUP re-reads the tokens file on SIGHUP and keeps the old set when the new one is invalid.
func reloadOnSIGHUP(ctx context.Context, tokens *auth.Store, path string, log *slog.Logger) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGHUP)
	defer signal.Stop(ch)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ch:
			if err := tokens.Load(path); err != nil {
				log.Error("token reload failed, keeping previous tokens", "error", err)
				continue
			}
			names := tokens.Names()
			log.Info("tokens reloaded", "count", len(names), "names", strings.Join(names, ","))
		}
	}
}
