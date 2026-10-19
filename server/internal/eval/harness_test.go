package eval

import (
	"fmt"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"

	"github.com/logos-messaging/logos-delivery-loss-monitor/server/internal/apierr"
	"github.com/logos-messaging/logos-delivery-loss-monitor/server/internal/config"
	"github.com/logos-messaging/logos-delivery-loss-monitor/server/internal/metrics"
)

const (
	t0       = int64(1_760_000_000_000)
	keyID    = "abcd1234"
	topicA   = "/lossmon/1/probe-0/raw"
	topicB   = "/lossmon/1/probe-1/raw"
	minuteMs = int64(60_000)
)

type harness struct {
	t   *testing.T
	cfg *config.Config
	c   *Core
	m   *metrics.Metrics
	now int64
	seq map[string]int64
	tok map[string]string
}

func newHarness(t *testing.T, mut ...func(*config.Config)) *harness {
	t.Helper()
	env := map[string]string{
		"LOSSMON_TOKENS_FILE": "tokens", "LOSSMON_PROBE_KEY_FILE": "key",
		"LOSSMON_TOPICS": topicA + "," + topicB,
	}
	cfg, err := config.Load(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range mut {
		f(cfg)
	}
	m := metrics.New(cfg.Topics, cfg.Locations, cfg.LossRatioWindow)
	return &harness{t: t, cfg: cfg, m: m, now: t0, c: NewCore(cfg, m, nil, keyID, t0), seq: map[string]int64{}, tok: map[string]string{}}
}

func uid(kind string, n int) string {
	prefix := map[string]string{"client": "c", "session": "5"}[kind]
	return fmt.Sprintf("%s0000000-0000-4000-8000-%012d", prefix, n)
}

func pid(i int) string  { return fmt.Sprintf("0x%032x", i+1) }
func hash(i int) string { return fmt.Sprintf("0x%064x", i+1) }

type regOpts struct {
	token  string
	client int
	mode   string
	role   string
	loc    string
	topics []string
	rate   float64
}

func (o regOpts) request(now int64) *RegisterRequest {
	if o.mode == "" {
		o.mode = "core"
	}
	if o.role == "" {
		o.role = "both"
	}
	if o.loc == "" {
		o.loc = "eu"
	}
	if o.topics == nil {
		o.topics = []string{topicA}
	}
	if o.rate == 0 {
		o.rate = 1
	}
	return &RegisterRequest{
		ClientID: uid("client", o.client), Mode: o.mode, Role: o.role, Location: o.loc, Preset: "logos.dev",
		ClusterID: 3, Topics: o.topics, RatePerTopic: o.rate, PayloadBytes: 1024, AnonymityLevel: "none",
		AppVersion: "0.1.0", LogosDeliveryVersion: "v0.39.2", ProbeKeyID: keyID, NTPSynced: true, ClientTimeMs: now,
	}
}

func (h *harness) tryRegister(sid int, o regOpts) (RegisterResult, *apierr.Error) {
	if o.token == "" {
		o.token = "op"
	}
	id := uid("session", sid)
	res, err := h.c.Register(h.now, o.token, id, o.request(h.now))
	if err == nil {
		h.tok[id] = o.token
	}
	return res, err
}

func (h *harness) register(sid int, o regOpts) string {
	h.t.Helper()
	if _, err := h.tryRegister(sid, o); err != nil {
		h.t.Fatalf("register %d: %v", sid, err)
	}
	return uid("session", sid)
}

func (h *harness) advance(d time.Duration) {
	h.now += d.Milliseconds()
	h.c.Tick(h.now)
}

func (h *harness) trySend(sid string, recs ...Record) (IngestResult, *apierr.Error) {
	for i := range recs {
		h.seq[sid]++
		recs[i].Seq = h.seq[sid]
	}
	return h.c.Ingest(h.now, h.tok[sid], sid, &Batch{SentAtMs: h.now, Records: recs})
}

func (h *harness) send(sid string, recs ...Record) {
	h.t.Helper()
	if _, err := h.trySend(sid, recs...); err != nil {
		h.t.Fatalf("ingest: %v", err)
	}
}

func (h *harness) skipSeq(sid string, n int) { h.seq[sid] += int64(n) }

func (h *harness) hb(topics ...string) Record {
	if len(topics) == 0 {
		topics = []string{topicA}
	}
	return Record{Type: typeHeartbeat, AtMs: h.now, Conn: "connected", Subscribed: topics}
}

func (h *harness) pub(i int) Record {
	return Record{Type: typePublish, AtMs: h.now, SendAtMs: h.now, ProbeID: pid(i), Hash: hash(i), Topic: topicA, Outcome: "propagated"}
}

func (h *harness) rcv(i int, source string) Record {
	return Record{Type: typeReceive, AtMs: h.now, ProbeID: pid(i), Hash: hash(i), Topic: topicA, Source: source, MsgTsMs: h.now}
}

func (h *harness) end() Record { return Record{Type: typeEnd, AtMs: h.now} }

// beats advances time in 10 s steps, sending a heartbeat from each session at every step.
func (h *harness) beats(d time.Duration, sids ...string) {
	h.t.Helper()
	for left := d; left > 0; left -= 10 * time.Second {
		step := 10 * time.Second
		if left < step {
			step = left
		}
		h.advance(step)
		for _, s := range sids {
			h.send(s, h.hb())
		}
	}
}

// settle runs long enough for any pair published now to be decided.
func (h *harness) settle(sids ...string) {
	h.t.Helper()
	h.beats(h.cfg.Window+h.cfg.Grace+20*time.Second, sids...)
}

type world struct {
	h       *harness
	p, a, b string
}

// newWorld registers a publisher and two receivers that are past their warmup.
func newWorld(t *testing.T, mut ...func(*config.Config)) *world {
	h := newHarness(t, mut...)
	w := &world{h: h}
	w.p = h.register(1, regOpts{client: 1, role: "publish", loc: "eu"})
	w.a = h.register(2, regOpts{client: 2, role: "receive", mode: "core", loc: "na"})
	w.b = h.register(3, regOpts{client: 3, role: "receive", mode: "edge", loc: "as"})
	for _, s := range []string{w.p, w.a, w.b} {
		h.send(s, h.hb())
	}
	h.beats(40*time.Second, w.p, w.a, w.b)
	return w
}

func (h *harness) families() []*dto.MetricFamily {
	fams, err := h.m.Registry.Gather()
	if err != nil {
		h.t.Fatal(err)
	}
	return fams
}

// sum adds every sample of a metric whose labels include all the given key/value pairs.
func (h *harness) sum(name string, kv ...string) float64 {
	var total float64
	for _, f := range h.families() {
		if f.GetName() != name {
			continue
		}
	next:
		for _, mt := range f.GetMetric() {
			for i := 0; i+1 < len(kv); i += 2 {
				found := false
				for _, l := range mt.GetLabel() {
					if l.GetName() == kv[i] && l.GetValue() == kv[i+1] {
						found = true
					}
				}
				if !found {
					continue next
				}
			}
			switch {
			case mt.Counter != nil:
				total += mt.Counter.GetValue()
			case mt.Gauge != nil:
				total += mt.Gauge.GetValue()
			case mt.Histogram != nil:
				total += float64(mt.Histogram.GetSampleCount())
			}
		}
	}
	return total
}

func (h *harness) seriesCount() int {
	n := 0
	for _, f := range h.families() {
		n += len(f.GetMetric())
	}
	return n
}

func (h *harness) expect(name string, want float64, kv ...string) {
	h.t.Helper()
	if got := h.sum(name, kv...); got != want {
		h.t.Errorf("%s%v = %v, want %v", name, kv, got, want)
	}
}
