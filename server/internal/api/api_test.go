package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/logos-messaging/logos-delivery-loss-monitor/server/internal/auth"
	"github.com/logos-messaging/logos-delivery-loss-monitor/server/internal/config"
	"github.com/logos-messaging/logos-delivery-loss-monitor/server/internal/eval"
	"github.com/logos-messaging/logos-delivery-loss-monitor/server/internal/metrics"
)

const (
	t0     = int64(1_760_000_000_000)
	topicA = "/lossmon/1/probe-0/raw"
	tokOp  = "operator-token-0123456789-abcdefghij"
	tokOp2 = "second-token-0123456789-abcdefghijkl"
	tokQ   = "query-token-0123456789-abcdefghijklm"
	keyHex = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"
)

type fakeClock struct{ ms atomic.Int64 }

func (f *fakeClock) NowMs() int64 { return f.ms.Load() }

type rig struct {
	t      *testing.T
	srv    *httptest.Server
	mux    *httptest.Server
	clock  *fakeClock
	engine *eval.Engine
	tokens *auth.Store
	tfile  string
	cfg    *config.Config
}

func newRig(t *testing.T, mut ...func(*config.Config)) *rig {
	t.Helper()
	dir := t.TempDir()
	tfile := filepath.Join(dir, "tokens")
	kfile := filepath.Join(dir, "key")
	writeTokens(t, tfile, "op report "+tokOp+"\nop2 report "+tokOp2+"\nreader query "+tokQ+"\n")
	if err := os.WriteFile(kfile, []byte(keyHex), 0o600); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"LOSSMON_TOKENS_FILE": tfile, "LOSSMON_PROBE_KEY_FILE": kfile, "LOSSMON_TOPICS": topicA}
	cfg, err := config.Load(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range mut {
		f(cfg)
	}
	key, err := config.LoadProbeKey(kfile)
	if err != nil {
		t.Fatal(err)
	}
	tokens := &auth.Store{}
	if err := tokens.Load(tfile); err != nil {
		t.Fatal(err)
	}
	m := metrics.New(cfg.Topics, cfg.Locations, cfg.LossRatioWindow)
	clock := &fakeClock{}
	clock.ms.Store(t0)
	core := eval.NewCore(cfg, m, nil, key.ID, t0)
	engine := eval.NewEngine(core, clock, nilLogger())
	ctx, cancel := context.WithCancel(context.Background())
	go engine.Run(ctx)
	s := New(cfg, tokens, engine, m, key, clock.NowMs, nilLogger())
	r := &rig{t: t, clock: clock, engine: engine, tokens: tokens, tfile: tfile, cfg: cfg}
	r.srv = httptest.NewServer(s.Handler())
	r.mux = httptest.NewServer(s.MetricsHandler())
	t.Cleanup(func() { r.srv.Close(); r.mux.Close(); cancel() })
	return r
}

func writeTokens(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (r *rig) do(method, path, token string, body any) (*http.Response, []byte) {
	r.t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		rd = strings.NewReader(b)
	case []byte:
		rd = bytes.NewReader(b)
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			r.t.Fatal(err)
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, r.srv.URL+path, rd)
	if err != nil {
		r.t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp, out
}

func (r *rig) status(method, path, token string, body any) int {
	r.t.Helper()
	resp, _ := r.do(method, path, token, body)
	return resp.StatusCode
}

func sid(n int) string { return fmt.Sprintf("50000000-0000-4000-8000-%012d", n) }
func cid(n int) string { return fmt.Sprintf("c0000000-0000-4000-8000-%012d", n) }

func (r *rig) regBody(client int, role string) map[string]any {
	return map[string]any{
		"client_id": cid(client), "mode": "core", "role": role, "location": "eu", "preset": "logos.dev",
		"cluster_id": 3, "topics": []string{topicA}, "rate_per_topic": 0.5, "payload_bytes": 1024,
		"ephemeral": false, "anonymity_level": "none", "reliability": false, "app_version": "0.1.0",
		"logos_delivery_version": "v0.39.2", "probe_key_id": "", "ntp_synced": true, "client_time_ms": r.clock.NowMs(),
	}
}

func (r *rig) keyID() string {
	_, body := r.do("GET", "/v1/config", tokOp, nil)
	var c map[string]any
	_ = json.Unmarshal(body, &c)
	return c["probe_key_id"].(string)
}

func (r *rig) register(n, client int, role, token string) *http.Response {
	r.t.Helper()
	b := r.regBody(client, role)
	b["probe_key_id"] = r.keyID()
	resp, body := r.do("PUT", "/v1/sessions/"+sid(n), token, b)
	if resp.StatusCode != 201 && resp.StatusCode != 200 {
		r.t.Fatalf("register: %d %s", resp.StatusCode, body)
	}
	return resp
}

func (r *rig) metricsText() string {
	resp, err := http.Get(r.mux.URL + "/metrics")
	if err != nil {
		r.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

// CS-009: report endpoints are protected and revocation takes effect on reload.
func TestCS009_AuthRequired(t *testing.T) {
	r := newRig(t)
	cases := []struct {
		name, token string
		want        int
	}{
		{"no token", "", 401},
		{"wrong token", "wrong-token-0123456789-abcdefghijklmn", 401},
		{"query scope on report endpoint", tokQ, 403},
	}
	for _, tc := range cases {
		for _, ep := range []struct{ method, path string }{
			{"GET", "/v1/config"}, {"PUT", "/v1/sessions/" + sid(1)}, {"POST", "/v1/sessions/" + sid(1) + "/records"},
		} {
			if got := r.status(ep.method, ep.path, tc.token, `{}`); got != tc.want {
				t.Errorf("%s %s %s: %d, want %d", tc.name, ep.method, ep.path, got, tc.want)
			}
		}
	}
	if r.status("POST", "/v1/sessions/"+sid(1)+"/records", "", `{"records":[{"seq":1,"type":"end","at_ms":1}]}`) != 401 {
		t.Fatal("unauthenticated post accepted")
	}
	if strings.Contains(r.metricsText(), `type="end"`) {
		t.Fatal("unauthenticated record was ingested")
	}

	r.register(1, 1, "receive", tokOp)
	writeTokens(t, r.tfile, "op2 report "+tokOp2+"\nreader query "+tokQ+"\n")
	if err := r.tokens.Load(r.tfile); err != nil {
		t.Fatal(err)
	}
	if got := r.status("GET", "/v1/config", tokOp, nil); got != 401 {
		t.Fatalf("revoked token: %d, want 401", got)
	}
	if got := r.status("GET", "/v1/config", tokOp2, nil); got != 200 {
		t.Fatalf("other token after reload: %d, want 200", got)
	}
}

func TestUnauthorizedBodyIsFixedAndCounted(t *testing.T) {
	r := newRig(t)
	resp, body := r.do("PUT", "/v1/sessions/"+sid(1), "", strings.Repeat("x", 2<<20))
	if resp.StatusCode != 401 || strings.TrimSpace(string(body)) != `{"error":"unauthorized"}` {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	if resp.Header.Get("WWW-Authenticate") == "" {
		t.Error("missing WWW-Authenticate")
	}
	if !strings.Contains(r.metricsText(), `lossmon_api_rejections_total{code="unauthorized"} 1`) {
		t.Error("rejection not counted")
	}
}

func TestConfigEndpoint(t *testing.T) {
	r := newRig(t)
	resp, body := r.do("GET", "/v1/config", tokOp, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	var c map[string]any
	if err := json.Unmarshal(body, &c); err != nil {
		t.Fatal(err)
	}
	if c["probe_key"] != "0x"+keyHex || len(c["probe_key_id"].(string)) != 8 || c["preset"] != "logos.dev" ||
		c["cluster_id"].(float64) != 3 || c["heartbeat_interval_ms"].(float64) != 10000 || c["max_batch_records"].(float64) != 2000 ||
		c["server_time_ms"].(float64) != float64(t0) {
		t.Fatalf("unexpected config: %s", body)
	}
}

func TestRegisterStatusCodes(t *testing.T) {
	r := newRig(t)
	if got := r.register(1, 1, "receive", tokOp).StatusCode; got != 201 {
		t.Fatalf("first PUT: %d", got)
	}
	r.clock.ms.Add(5000)
	b := r.regBody(1, "receive")
	b["probe_key_id"] = r.keyID()
	if got := r.status("PUT", "/v1/sessions/"+sid(1), tokOp, b); got != 200 {
		t.Fatalf("repeat PUT with newer client_time_ms: %d", got)
	}
	b["rate_per_topic"] = 0.25
	resp, body := r.do("PUT", "/v1/sessions/"+sid(1), tokOp, b)
	if resp.StatusCode != 409 || !strings.Contains(string(body), "session_conflict") {
		t.Fatalf("changed PUT: %d %s", resp.StatusCode, body)
	}
	b = r.regBody(2, "receive")
	b["probe_key_id"] = r.keyID()
	if got := r.status("PUT", "/v1/sessions/"+sid(1), tokOp2, b); got != 403 {
		t.Fatalf("other token PUT: %d", got)
	}
	if got := r.status("PUT", "/v1/sessions/not-a-uuid", tokOp, b); got != 400 {
		t.Fatalf("bad id: %d", got)
	}
	if got := r.status("PUT", "/v1/sessions/"+sid(3), tokOp, `{not json`); got != 400 {
		t.Fatalf("bad json: %d", got)
	}
	b["preset"] = "logos.test"
	if got := r.status("PUT", "/v1/sessions/"+sid(3), tokOp, b); got != 422 {
		t.Fatalf("network mismatch: %d", got)
	}
}

func TestRecordsFlowAndLimits(t *testing.T) {
	r := newRig(t)
	r.register(1, 1, "both", tokOp)
	hb := func(seq int) map[string]any {
		return map[string]any{"seq": seq, "type": "heartbeat", "at_ms": r.clock.NowMs(), "conn": "connected", "subscribed": []string{topicA}}
	}
	batch := func(recs ...map[string]any) map[string]any {
		return map[string]any{"sent_at_ms": r.clock.NowMs(), "records": recs}
	}
	resp, body := r.do("POST", "/v1/sessions/"+sid(1)+"/records", tokOp, batch(hb(1), hb(2)))
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"acked_seq":2`) {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	if got := r.status("POST", "/v1/sessions/"+sid(9)+"/records", tokOp, batch(hb(1))); got != 404 {
		t.Fatalf("unknown session: %d", got)
	}
	if got := r.status("POST", "/v1/sessions/"+sid(1)+"/records", tokOp2, batch(hb(3))); got != 403 {
		t.Fatalf("foreign session: %d", got)
	}
	if got := r.status("POST", "/v1/sessions/"+sid(1)+"/records", tokOp, `[1,2`); got != 400 {
		t.Fatalf("bad json: %d", got)
	}
	big := batch(hb(3))
	big["pad"] = strings.Repeat("a", 2<<20)
	resp, body = r.do("POST", "/v1/sessions/"+sid(1)+"/records", tokOp, big)
	if resp.StatusCode != 413 || !strings.Contains(string(body), "body_too_large") {
		t.Fatalf("big body: %d %s", resp.StatusCode, body)
	}
	many := make([]map[string]any, 2001)
	for i := range many {
		many[i] = hb(10 + i)
	}
	if got := r.status("POST", "/v1/sessions/"+sid(1)+"/records", tokOp, batch(many...)); got != 413 {
		t.Fatalf("batch of 2001 records: %d", got)
	}
	if got := r.status("POST", "/v1/sessions/"+sid(1)+"/records", tokOp, map[string]any{"sent_at_ms": -4, "records": []int{}}); got != 422 {
		t.Fatalf("bad sent_at_ms: %d", got)
	}
}

func TestRequestRateLimitHas429WithRetryAfter(t *testing.T) {
	r := newRig(t, func(c *config.Config) { c.TokenReqPerSec = 1 })
	var limited *http.Response
	var body []byte
	for i := 0; i < 5 && limited == nil; i++ {
		resp, b := r.do("GET", "/v1/config", tokOp, nil)
		if resp.StatusCode == 429 {
			limited, body = resp, b
		}
	}
	if limited == nil {
		t.Fatal("never rate limited")
	}
	if limited.Header.Get("Retry-After") == "" || !strings.Contains(string(body), `"retry_after_ms"`) || !strings.Contains(string(body), "rate_limited") {
		t.Fatalf("429 shape: %v %s", limited.Header, body)
	}
	if got := r.status("GET", "/v1/config", tokOp2, nil); got != 200 {
		t.Fatalf("limits must be per token, got %d", got)
	}
}

func TestRecordRateLimit429(t *testing.T) {
	r := newRig(t, func(c *config.Config) { c.TokenRecordsPerSec = 1 })
	r.register(1, 1, "receive", tokOp)
	recs := make([]map[string]any, 100)
	for i := range recs {
		recs[i] = map[string]any{"seq": i + 1, "type": "heartbeat", "at_ms": r.clock.NowMs(), "conn": "connected", "subscribed": []string{topicA}}
	}
	body := map[string]any{"sent_at_ms": r.clock.NowMs(), "records": recs}
	var resp *http.Response
	var raw []byte
	for i := 0; i < 3; i++ {
		resp, raw = r.do("POST", "/v1/sessions/"+sid(1)+"/records", tokOp, body)
		if resp.StatusCode == 429 {
			break
		}
	}
	if resp.StatusCode != 429 || !strings.Contains(string(raw), "record_rate_limited") || resp.Header.Get("Retry-After") == "" {
		t.Fatalf("%d %s %v", resp.StatusCode, raw, resp.Header)
	}
}

func TestEndToEndLossOverHTTP(t *testing.T) {
	r := newRig(t)
	r.register(1, 1, "publish", tokOp)
	r.register(2, 2, "receive", tokOp)
	r.register(3, 3, "receive", tokOp2)
	post := func(n int, recs ...map[string]any) {
		resp, body := r.do("POST", "/v1/sessions/"+sid(n)+"/records", map[int]string{1: tokOp, 2: tokOp, 3: tokOp2}[n],
			map[string]any{"sent_at_ms": r.clock.NowMs(), "records": recs})
		if resp.StatusCode != 200 {
			t.Fatalf("post: %d %s", resp.StatusCode, body)
		}
	}
	seq := map[int]int{}
	rec := func(n int, m map[string]any) map[string]any {
		seq[n]++
		m["seq"] = seq[n]
		if _, ok := m["at_ms"]; !ok {
			m["at_ms"] = r.clock.NowMs()
		}
		return m
	}
	hb := func(n int) map[string]any {
		return rec(n, map[string]any{"type": "heartbeat", "conn": "connected", "subscribed": []string{topicA}})
	}
	tick := func() {
		if err := r.engine.Do(context.Background(), func(c *eval.Core, now int64) { c.Tick(now) }); err != nil {
			t.Fatal(err)
		}
	}
	post(2, hb(2))
	post(3, hb(3))
	for i := 0; i < 8; i++ {
		r.clock.ms.Add(10_000)
		post(2, hb(2))
		post(3, hb(3))
		tick()
	}
	probe := "0x" + strings.Repeat("0", 31) + "1"
	hash := "0x" + strings.Repeat("0", 63) + "1"
	post(1, rec(1, map[string]any{"type": "publish", "send_at_ms": r.clock.NowMs(), "probe_id": probe, "hash": hash, "topic": topicA, "outcome": "propagated"}))
	post(2, rec(2, map[string]any{"type": "receive", "probe_id": probe, "hash": hash, "topic": topicA, "source": "live", "msg_ts_ms": r.clock.NowMs()}))
	for i := 0; i < 66; i++ {
		r.clock.ms.Add(10_000)
		post(2, hb(2))
		post(3, hb(3))
		tick()
	}
	text := r.metricsText()
	for _, want := range []string{"lossmon_expected_total{", "lossmon_lost_total{", "lossmon_delivered_total{", "lossmon_active_sessions{"} {
		if !strings.Contains(text, want) {
			t.Errorf("metrics missing %s", want)
		}
	}
	if !strings.Contains(text, `lossmon_lost_total{content_topic="/lossmon/1/probe-0/raw",publisher_anonymity="none",publisher_location="eu",publisher_mode="core",receiver_location="eu",receiver_mode="core"} 1`) {
		t.Errorf("expected one lost pair for the silent receiver:\n%s", text)
	}
}

func TestMetricsAndHealthzAreNotOnTheAPIPort(t *testing.T) {
	r := newRig(t)
	for _, p := range []string{"/metrics", "/healthz", "/v1/stats", "/nope"} {
		if got := r.status("GET", p, tokOp, nil); got != 404 {
			t.Errorf("%s on API port: %d, want 404", p, got)
		}
	}
	resp, err := http.Get(r.mux.URL + "/healthz")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("healthz: %v %v", resp, err)
	}
	resp.Body.Close()
}

func TestHTTPServerTimeouts(t *testing.T) {
	r := newRig(t)
	_ = r
	s := New(r.cfg, r.tokens, r.engine, metrics.New(r.cfg.Topics, r.cfg.Locations, r.cfg.LossRatioWindow), config.ProbeKey{}, r.clock.NowMs, nilLogger())
	h := s.HTTPServer(":0", s.Handler())
	if h.ReadHeaderTimeout.Seconds() != 5 || h.ReadTimeout.Seconds() != 30 || h.WriteTimeout.Seconds() != 30 ||
		h.IdleTimeout.Seconds() != 60 || h.MaxHeaderBytes != 16<<10 {
		t.Fatalf("timeouts: %+v", h)
	}
}

func TestXForwardedForIsIgnored(t *testing.T) {
	r := newRig(t, func(c *config.Config) { c.TokenReqPerSec = 1 })
	req, _ := http.NewRequest("GET", r.srv.URL+"/v1/config", nil)
	req.Header.Set("Authorization", "Bearer "+tokOp)
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("%v %v", resp, err)
	}
	resp.Body.Close()
}
