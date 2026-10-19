// Package metrics owns the Prometheus registry. Every label value is mapped through a fixed
// allowlist or enum, so the series count is bounded whatever clients send.
package metrics

import (
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/logos-messaging/logos-delivery-loss-monitor/server/internal/apierr"
)

const (
	namespace = "lossmon"
	other     = "other"
)

// Indeterminate reasons.
const (
	ReasonClockSkew       = "clock_skew"
	ReasonReceiverEnded   = "receiver_ended"
	ReasonReceiverGap     = "receiver_gap"
	ReasonReceiverLagging = "receiver_lagging"
	ReasonEvicted         = "evicted"
	ReasonRejected        = "rejected"
	ReasonReplayed        = "replayed"
)

// Skipped reasons.
const (
	SkipBeforeStart        = "before_start"
	SkipBeforeRegistration = "before_registration"
	SkipPublishReportLate  = "publish_report_late"
	SkipEvicted            = "evicted"
)

var (
	modes         = []string{"core", "edge"}
	anonymities   = []string{"none", "preferred", "required"}
	sources       = []string{"live", "history"}
	reasons       = []string{ReasonClockSkew, ReasonReceiverEnded, ReasonReceiverGap, ReasonReceiverLagging, ReasonEvicted, ReasonRejected, ReasonReplayed}
	skipReasons   = []string{SkipBeforeStart, SkipBeforeRegistration, SkipPublishReportLate, SkipEvicted}
	sendOutcomes  = []string{"error", "timeout"}
	recordTypes   = []string{"heartbeat", "publish", "receive", "end"}
	recordResults = []string{"ok", "duplicate_seq", "invalid", "over_budget"}
	gapReasons    = []string{ReasonReceiverGap, ReasonRejected, ReasonEvicted}
	connStatuses  = []string{"connected", "partially_connected", "disconnected", "unknown"}
)

// PairLabels identifies a (publish, receiver) pair class.
type PairLabels struct {
	Topic, PubMode, RecvMode, PubLoc, RecvLoc, PubAnon string
}

// PubLabels identifies a publisher class.
type PubLabels struct {
	Topic, Mode, Loc, Anon string
}

// SessionKey identifies a bucket of live sessions in the active-sessions gauge.
type SessionKey struct {
	Mode, Loc, Conn string
}

// Metrics is safe for concurrent use. Loss-ratio state is touched only by the evaluation goroutine.
type Metrics struct {
	Registry *prometheus.Registry

	topics, locations map[string]bool

	expected, delivered, lost, late, duplicate, indeterminate *prometheus.CounterVec
	lossRatio                                                 *prometheus.GaugeVec
	published, sendQueued, sendFailed                         *prometheus.CounterVec
	unmatched, skipped, clientDropped                         *prometheus.CounterVec
	latency                                                   *prometheus.HistogramVec
	skewed, pending, orphans                                  prometheus.Gauge
	ingest, foreign, unverified, replayed                     *prometheus.CounterVec
	gaps, rejections                                          *prometheus.CounterVec

	sessions *sessionsCollector

	ratioMinutes int64
	rings        map[string]*ring
	dirty        map[string]bool
	lastMinute   int64
	tickMinute   int64
}

// New builds the registry. topics and locations are the configured allowlists.
func New(topics, locations []string, lossRatioWindow time.Duration) *Metrics {
	m := &Metrics{
		Registry:  prometheus.NewRegistry(),
		topics:    set(topics),
		locations: set(locations),
		rings:     map[string]*ring{},
		dirty:     map[string]bool{},
	}
	m.ratioMinutes = int64(lossRatioWindow / time.Minute)
	if m.ratioMinutes < 1 {
		m.ratioMinutes = 1
	}
	pair := []string{"content_topic", "publisher_mode", "receiver_mode", "publisher_location", "receiver_location", "publisher_anonymity"}
	pub := []string{"content_topic", "publisher_mode", "publisher_location", "publisher_anonymity"}
	cv := func(name, help string, labels ...string) *prometheus.CounterVec {
		return prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: name, Help: help}, labels)
	}
	gauge := func(name, help string) prometheus.Gauge {
		return prometheus.NewGauge(prometheus.GaugeOpts{Namespace: namespace, Name: name, Help: help})
	}
	m.expected = cv("expected_total", "Pairs that were delivered or lost.", pair...)
	m.delivered = cv("delivered_total", "Pairs delivered within the window.", append(append([]string{}, pair...), "source")...)
	m.lost = cv("lost_total", "Pairs not delivered within the window.", pair...)
	m.late = cv("late_total", "Receipts that arrived after the pair was decided lost.", pair...)
	m.duplicate = cv("duplicate_total", "Repeated receipts for one pair.", pair...)
	m.indeterminate = cv("indeterminate_total", "Pairs excluded from loss.", append(append([]string{}, pair...), "reason")...)
	m.lossRatio = prometheus.NewGaugeVec(prometheus.GaugeOpts{Namespace: namespace, Name: "message_loss_ratio", Help: "lost / (lost + delivered) over the rolling window."}, pair)
	m.published = cv("published_total", "Admitted publishes.", pub...)
	m.sendQueued = cv("send_queued_total", "Publishes held by the rate limiter.", pub...)
	m.sendFailed = cv("send_failed_total", "Publishes that failed to send.", append(append([]string{}, pub...), "outcome")...)
	m.unmatched = cv("unmatched_total", "Receipts with no publish report.", "content_topic")
	m.skipped = cv("skipped_total", "Records not evaluated.", "reason")
	m.clientDropped = cv("client_reports_dropped_total", "Records dropped by client buffers.", "mode", "location")
	m.latency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace, Name: "latency_seconds", Help: "Receive time minus publish send time, both clock-corrected.",
		Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 600},
	}, []string{"publisher_mode", "receiver_mode", "source"})
	m.skewed = gauge("sessions_clock_skewed", "Live sessions with a skewed clock.")
	m.pending = gauge("pending_publishes", "Publishes awaiting a decision.")
	m.orphans = gauge("pending_orphans", "Receipt groups awaiting their publish report.")
	m.ingest = cv("ingest_records_total", "Records processed by the ingest path.", "type", "result")
	m.foreign = cv("foreign_received_total", "Messages without the probe marker seen by receivers.", "receiver_mode", "receiver_location")
	m.unverified = cv("unverified_probes_total", "Probe-marked messages whose tag failed verification.", "receiver_mode", "receiver_location")
	m.replayed = cv("replayed_total", "Receipts replayed with a different hash or beyond the per-probe cap.", "content_topic")
	m.gaps = cv("coverage_gaps_total", "Coverage gaps opened on sessions.", "reason")
	m.rejections = cv("api_rejections_total", "API requests answered with an error.", "code")
	m.sessions = newSessionsCollector()

	m.Registry.MustRegister(m.expected, m.delivered, m.lost, m.late, m.duplicate, m.indeterminate, m.lossRatio,
		m.published, m.sendQueued, m.sendFailed, m.unmatched, m.skipped, m.clientDropped, m.latency,
		m.skewed, m.pending, m.orphans, m.ingest, m.foreign, m.unverified, m.replayed, m.gaps, m.rejections, m.sessions)
	return m
}

func set(vs []string) map[string]bool {
	s := make(map[string]bool, len(vs))
	for _, v := range vs {
		s[v] = true
	}
	return s
}

func enum(v string, allowed []string) string {
	for _, a := range allowed {
		if v == a {
			return v
		}
	}
	return other
}

func (m *Metrics) topic(v string) string {
	if m.topics[v] {
		return v
	}
	return other
}

// Location maps a self-reported location to its allowlisted label, or "other".
func (m *Metrics) Location(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if m.locations[v] {
		return v
	}
	return other
}

func (m *Metrics) pairValues(l PairLabels) []string {
	return []string{m.topic(l.Topic), enum(l.PubMode, modes), enum(l.RecvMode, modes), m.Location(l.PubLoc), m.Location(l.RecvLoc), enum(l.PubAnon, anonymities)}
}

func (m *Metrics) pubValues(l PubLabels) []string {
	return []string{m.topic(l.Topic), enum(l.Mode, modes), m.Location(l.Loc), enum(l.Anon, anonymities)}
}

func add(c *prometheus.CounterVec, n int, vals ...string) {
	if n > 0 {
		c.WithLabelValues(vals...).Add(float64(n))
	}
}

// Delivered records a delivered pair. It also counts as expected.
func (m *Metrics) Delivered(l PairLabels, source string) {
	v := m.pairValues(l)
	m.expected.WithLabelValues(v...).Inc()
	m.delivered.WithLabelValues(append(append([]string{}, v...), enum(source, sources))...).Inc()
	m.ratioAdd(v, false)
}

// Lost records a lost pair. It also counts as expected.
func (m *Metrics) Lost(l PairLabels) {
	v := m.pairValues(l)
	m.expected.WithLabelValues(v...).Inc()
	m.lost.WithLabelValues(v...).Inc()
	m.ratioAdd(v, true)
}

// Late records a receipt that arrived after its pair was decided lost.
func (m *Metrics) Late(l PairLabels) { m.late.WithLabelValues(m.pairValues(l)...).Inc() }

// Duplicate records n repeated receipts for one pair.
func (m *Metrics) Duplicate(l PairLabels, n int) { add(m.duplicate, n, m.pairValues(l)...) }

// Indeterminate records a pair excluded from loss.
func (m *Metrics) Indeterminate(l PairLabels, reason string) {
	m.indeterminate.WithLabelValues(append(m.pairValues(l), enum(reason, reasons))...).Inc()
}

// Published records an admitted publish.
func (m *Metrics) Published(l PubLabels) { m.published.WithLabelValues(m.pubValues(l)...).Inc() }

// SendQueued records a publish held by the sender's rate limiter.
func (m *Metrics) SendQueued(l PubLabels) { m.sendQueued.WithLabelValues(m.pubValues(l)...).Inc() }

// SendFailed records a publish that errored or timed out on the client.
func (m *Metrics) SendFailed(l PubLabels, outcome string) {
	m.sendFailed.WithLabelValues(append(m.pubValues(l), enum(outcome, sendOutcomes))...).Inc()
}

// Unmatched records n receipts that never found their publish.
func (m *Metrics) Unmatched(topic string, n int) { add(m.unmatched, n, m.topic(topic)) }

// Skipped records n records left out of evaluation.
func (m *Metrics) Skipped(reason string, n int) { add(m.skipped, n, enum(reason, skipReasons)) }

// ClientDropped records n records dropped in a client buffer.
func (m *Metrics) ClientDropped(mode, loc string, n int) {
	add(m.clientDropped, n, enum(mode, modes), m.Location(loc))
}

// Latency records one delivery latency in seconds.
func (m *Metrics) Latency(pubMode, recvMode, source string, seconds float64) {
	m.latency.WithLabelValues(enum(pubMode, modes), enum(recvMode, modes), enum(source, sources)).Observe(seconds)
}

// IngestRecord counts one processed record.
func (m *Metrics) IngestRecord(typ, result string) {
	m.ingest.WithLabelValues(enum(typ, recordTypes), enum(result, recordResults)).Inc()
}

// Foreign records n non-probe messages seen by a receiver.
func (m *Metrics) Foreign(mode, loc string, n int) {
	add(m.foreign, n, enum(mode, modes), m.Location(loc))
}

// Unverified records n probe-marked messages that failed tag verification.
func (m *Metrics) Unverified(mode, loc string, n int) {
	add(m.unverified, n, enum(mode, modes), m.Location(loc))
}

// Replayed records n replayed receipts.
func (m *Metrics) Replayed(topic string, n int) { add(m.replayed, n, m.topic(topic)) }

// CoverageGap counts a coverage gap opened on a session.
func (m *Metrics) CoverageGap(reason string) { m.gaps.WithLabelValues(enum(reason, gapReasons)).Inc() }

// APIRejection counts an API error response by its fixed code.
func (m *Metrics) APIRejection(code string) {
	m.rejections.WithLabelValues(enum(code, apierr.Codes)).Inc()
}

// SetGauges publishes the state gauges.
func (m *Metrics) SetGauges(skewed, pending, orphans int) {
	m.skewed.Set(float64(skewed))
	m.pending.Set(float64(pending))
	m.orphans.Set(float64(orphans))
}

// SetActiveSessions replaces the active-sessions snapshot.
func (m *Metrics) SetActiveSessions(counts map[SessionKey]int) {
	clean := make(map[SessionKey]int, len(counts))
	for k, n := range counts {
		clean[SessionKey{enum(k.Mode, modes), m.Location(k.Loc), enum(k.Conn, connStatuses)}] += n
	}
	m.sessions.set(clean)
}

type sessionsCollector struct {
	mu     sync.Mutex
	counts map[SessionKey]int
	desc   *prometheus.Desc
}

func newSessionsCollector() *sessionsCollector {
	return &sessionsCollector{
		desc: prometheus.NewDesc(namespace+"_active_sessions", "Live sessions.", []string{"mode", "location", "connection_status"}, nil),
	}
}

func (c *sessionsCollector) set(counts map[SessionKey]int) {
	c.mu.Lock()
	c.counts = counts
	c.mu.Unlock()
}

func (c *sessionsCollector) Describe(ch chan<- *prometheus.Desc) { ch <- c.desc }

func (c *sessionsCollector) Collect(ch chan<- prometheus.Metric) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, n := range c.counts {
		ch <- prometheus.MustNewConstMetric(c.desc, prometheus.GaugeValue, float64(n), k.Mode, k.Loc, k.Conn)
	}
}

type slot struct {
	minute      int64
	lost, deliv int
}

type ring struct {
	labels []string
	slots  []slot
}

func (m *Metrics) ratioAdd(labels []string, lost bool) {
	key := strings.Join(labels, "\x00")
	r := m.rings[key]
	if r == nil {
		r = &ring{labels: labels, slots: make([]slot, m.ratioMinutes)}
		m.rings[key] = r
	}
	minute := m.lastMinute
	s := &r.slots[minute%m.ratioMinutes]
	if s.minute != minute {
		*s = slot{minute: minute}
	}
	if lost {
		s.lost++
	} else {
		s.deliv++
	}
	m.dirty[key] = true
}

// Tick refreshes the loss-ratio gauges for time nowMs. Call it from the evaluation goroutine.
func (m *Metrics) Tick(nowMs int64) {
	cur := nowMs / 60000
	m.lastMinute = cur
	if cur != m.tickMinute {
		m.tickMinute = cur
		for k := range m.rings {
			m.dirty[k] = true
		}
	}
	for key := range m.dirty {
		r := m.rings[key]
		var lost, deliv int
		for _, s := range r.slots {
			if s.minute > cur-m.ratioMinutes && s.minute <= cur {
				lost += s.lost
				deliv += s.deliv
			}
		}
		if lost+deliv == 0 {
			m.lossRatio.DeleteLabelValues(r.labels...)
			delete(m.rings, key)
		} else {
			m.lossRatio.WithLabelValues(r.labels...).Set(float64(lost) / float64(lost+deliv))
		}
		delete(m.dirty, key)
	}
}

// SetNow sets the clock used to bucket loss-ratio samples. Call it before recording decisions.
func (m *Metrics) SetNow(nowMs int64) { m.lastMinute = nowMs / 60000 }
