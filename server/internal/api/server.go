// Package api serves the /v1 REST API. Authentication and rate limiting read headers only,
// before any request body is touched.
package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/logos-messaging/logos-delivery-loss-monitor/server/internal/apierr"
	"github.com/logos-messaging/logos-delivery-loss-monitor/server/internal/auth"
	"github.com/logos-messaging/logos-delivery-loss-monitor/server/internal/config"
	"github.com/logos-messaging/logos-delivery-loss-monitor/server/internal/eval"
	"github.com/logos-messaging/logos-delivery-loss-monitor/server/internal/metrics"
	"github.com/logos-messaging/logos-delivery-loss-monitor/server/internal/ratelimit"
)

// Server wires the REST handlers to the evaluation engine.
type Server struct {
	cfg      *config.Config
	tokens   *auth.Store
	engine   *eval.Engine
	m        *metrics.Metrics
	probeKey config.ProbeKey
	reqLimit *ratelimit.Keyed
	now      func() int64
	log      *slog.Logger
}

// New builds a Server. now supplies Unix milliseconds for the request rate limiter.
func New(cfg *config.Config, tokens *auth.Store, engine *eval.Engine, m *metrics.Metrics, key config.ProbeKey, now func() int64, log *slog.Logger) *Server {
	return &Server{
		cfg: cfg, tokens: tokens, engine: engine, m: m, probeKey: key, now: now, log: log,
		reqLimit: ratelimit.NewKeyed(cfg.TokenReqPerSec, 2*cfg.TokenReqPerSec),
	}
}

// Handler returns the /v1 API handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/config", s.authed(auth.ScopeReport, s.handleConfig))
	mux.HandleFunc("PUT /v1/sessions/{id}", s.authed(auth.ScopeReport, s.handleRegister))
	mux.HandleFunc("POST /v1/sessions/{id}/records", s.authed(auth.ScopeReport, s.handleRecords))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { s.fail(w, notFound(r)) })
	return s.recoverer(mux)
}

// HTTPServer returns an http.Server for addr with the timeouts the contract requires.
func (s *Server) HTTPServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr: addr, Handler: h,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second,
		WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second,
		MaxHeaderBytes: 16 << 10,
		ErrorLog:       slog.NewLogLogger(s.log.Handler(), slog.LevelDebug),
	}
}

// MetricsHandler serves /metrics and /healthz for the internal port.
func (s *Server) MetricsHandler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.HandlerFor(s.m.Registry, promhttp.HandlerOpts{}))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok\n"))
	})
	return mux
}

func notFound(r *http.Request) *apierr.Error {
	return apierr.New(http.StatusNotFound, apierr.NotFound, "no such endpoint")
}

type errBody struct {
	Error        string `json:"error"`
	Detail       string `json:"detail,omitempty"`
	RetryAfterMs int64  `json:"retry_after_ms,omitempty"`
}

func (s *Server) fail(w http.ResponseWriter, e *apierr.Error) {
	s.m.APIRejection(e.Code)
	body := errBody{Error: e.Code, Detail: e.Detail, RetryAfterMs: e.RetryAfterMs}
	if e.RetryAfterMs > 0 {
		w.Header().Set("Retry-After", strconv.FormatInt((e.RetryAfterMs+999)/1000, 10))
	}
	writeJSON(w, e.Status, body)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.log.Error("handler panic")
				s.fail(w, apierr.New(http.StatusInternalServerError, apierr.Internal, "internal error"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type authedHandler func(w http.ResponseWriter, r *http.Request, id auth.Identity)

// authed checks the bearer token, its scope and the per-token request rate from headers alone.
func (s *Server) authed(scope auth.Scope, h authedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok, ok := bearer(r.Header.Get("Authorization"))
		var id auth.Identity
		if ok {
			id, ok = s.tokens.Lookup(tok)
		}
		if !ok {
			w.Header().Set("WWW-Authenticate", "Bearer")
			s.fail(w, &apierr.Error{Status: http.StatusUnauthorized, Code: apierr.Unauthorized})
			return
		}
		if id.Scope != scope {
			s.fail(w, apierr.New(http.StatusForbidden, apierr.Forbidden, "token scope does not allow this endpoint"))
			return
		}
		if ok, wait := s.reqLimit.Take(id.Name, s.now(), 1); !ok {
			s.fail(w, apierr.Retry(apierr.RateLimited, wait))
			return
		}
		h(w, r, id)
	}
}

func bearer(h string) (string, bool) {
	const prefix = "Bearer "
	if len(h) <= len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return "", false
	}
	return strings.TrimSpace(h[len(prefix):]), true
}

// decode reads a size-limited JSON body into v and maps failures to API errors.
func (s *Server) decode(w http.ResponseWriter, r *http.Request, v any) *apierr.Error {
	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return apierr.New(http.StatusRequestEntityTooLarge, apierr.BodyTooLarge, "request body exceeds the limit")
		}
		return apierr.New(http.StatusBadRequest, apierr.BadJSON, "request body is not valid JSON")
	}
	return nil
}

type configResponse struct {
	ProbeKey          string   `json:"probe_key"`
	ProbeKeyID        string   `json:"probe_key_id"`
	Preset            string   `json:"preset"`
	ClusterID         int      `json:"cluster_id"`
	Topics            []string `json:"topics"`
	Locations         []string `json:"locations"`
	Ephemeral         bool     `json:"ephemeral"`
	MaxRatePerClient  float64  `json:"max_rate_per_client"`
	MaxPayloadBytes   int      `json:"max_payload_bytes"`
	HeartbeatInterval int64    `json:"heartbeat_interval_ms"`
	MaxBatchRecords   int      `json:"max_batch_records"`
	ReceiptMaxAgeMs   int64    `json:"receipt_max_age_ms"`
	MaxClockOffsetMs  int64    `json:"max_clock_offset_ms"`
	ServerTimeMs      int64    `json:"server_time_ms"`
}

func (s *Server) handleConfig(w http.ResponseWriter, _ *http.Request, _ auth.Identity) {
	writeJSON(w, http.StatusOK, configResponse{
		ProbeKey: s.probeKey.Hex(), ProbeKeyID: s.probeKey.ID, Preset: s.cfg.Preset, ClusterID: s.cfg.ClusterID,
		Topics: s.cfg.Topics, Locations: s.cfg.Locations, Ephemeral: s.cfg.Ephemeral,
		MaxRatePerClient: s.cfg.MaxRatePerClient, MaxPayloadBytes: s.cfg.MaxPayloadBytes,
		HeartbeatInterval: s.cfg.HeartbeatInterval.Milliseconds(), MaxBatchRecords: s.cfg.MaxBatchRecords,
		ReceiptMaxAgeMs: s.cfg.ReceiptMaxAgeMs(), MaxClockOffsetMs: s.cfg.MaxClockOffsetMs(), ServerTimeMs: s.now(),
	})
}

func (s *Server) sessionID(r *http.Request) (string, *apierr.Error) {
	id := r.PathValue("id")
	if !eval.IsUUID(id) {
		return "", apierr.New(http.StatusBadRequest, apierr.BadSessionID, "session id must be a lowercase UUID")
	}
	return id, nil
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request, who auth.Identity) {
	id, e := s.sessionID(r)
	if e != nil {
		s.fail(w, e)
		return
	}
	var req eval.RegisterRequest
	if e := s.decode(w, r, &req); e != nil {
		s.fail(w, e)
		return
	}
	res, e := s.engine.Register(r.Context(), who.Name, id, &req)
	if e != nil {
		s.fail(w, e)
		return
	}
	status := http.StatusOK
	if res.Created {
		status = http.StatusCreated
	}
	writeJSON(w, status, res)
}

func (s *Server) handleRecords(w http.ResponseWriter, r *http.Request, who auth.Identity) {
	id, e := s.sessionID(r)
	if e != nil {
		s.fail(w, e)
		return
	}
	var b eval.Batch
	if e := s.decode(w, r, &b); e != nil {
		s.fail(w, e)
		return
	}
	if len(b.Records) > s.cfg.MaxBatchRecords {
		s.fail(w, apierr.New(http.StatusRequestEntityTooLarge, apierr.BatchTooLarge, "too many records in one batch"))
		return
	}
	res, e := s.engine.Ingest(r.Context(), who.Name, id, &b)
	if e != nil {
		s.fail(w, e)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
