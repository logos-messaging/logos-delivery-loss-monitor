package eval

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/logos-messaging/logos-delivery-loss-monitor/server/internal/config"
)

// SEC-003: filling the orphan cap evicts genuine receipts, and their pairs must not become lost.
func TestSEC003_OrphanEvictionNeverProducesLost(t *testing.T) {
	w := newWorld(t, func(c *config.Config) { c.MaxOrphans = 20 })
	h := w.h
	h.advance(time.Second)
	h.send(w.a, h.rcv(0, "live"))
	h.send(w.b, h.rcv(0, "live"))
	for i := 1; i <= 100; i++ {
		h.send(w.a, h.rcv(1000+i, "live"))
	}
	if h.c.orphanCount > 20 {
		t.Fatalf("orphan count %d exceeds cap", h.c.orphanCount)
	}
	h.send(w.p, h.pub(0))
	h.settle(w.p, w.a, w.b)
	h.expect("lossmon_lost_total", 0)
	h.expect("lossmon_indeterminate_total", 2, "reason", "evicted")
	h.expect("lossmon_skipped_total", 82, "reason", "evicted")
	h.expect("lossmon_coverage_gaps_total", 82, "reason", "evicted")
}

func TestSEC003_PendingEvictionNeverProducesLost(t *testing.T) {
	w := newWorld(t, func(c *config.Config) { c.MaxPendingPublishes = 3 })
	h := w.h
	for i := 0; i < 10; i++ {
		h.advance(time.Second)
		h.send(w.p, h.pub(i))
	}
	h.expect("lossmon_pending_publishes", 3)
	for i := 7; i < 10; i++ {
		h.send(w.a, h.rcv(i, "live"))
		h.send(w.b, h.rcv(i, "live"))
	}
	h.settle(w.p, w.a, w.b)
	h.expect("lossmon_lost_total", 0)
	h.expect("lossmon_indeterminate_total", 14, "reason", "evicted")
	h.expect("lossmon_delivered_total", 6)
}

func TestSEC003_TombstoneEvictionNeverProducesLost(t *testing.T) {
	w := newWorld(t, func(c *config.Config) { c.MaxPendingPublishes = 2 })
	h := w.h
	for i := 0; i < 2; i++ {
		h.advance(time.Second)
		h.send(w.p, h.pub(i))
		h.send(w.a, h.rcv(i, "live"))
		h.send(w.b, h.rcv(i, "live"))
	}
	h.settle(w.p, w.a, w.b)
	for i := 2; i < 6; i++ {
		h.advance(time.Second)
		h.send(w.p, h.pub(i))
		h.send(w.a, h.rcv(i, "live"))
		h.send(w.b, h.rcv(i, "live"))
	}
	h.settle(w.p, w.a, w.b)
	h.send(w.a, h.rcv(0, "history"))
	h.expect("lossmon_lost_total", 0)
	h.expect("lossmon_late_total", 0)
}

// SEC-004: dropped records open a gap; a seq jump with intact heartbeats is never loss.
func TestSEC004_SeqGapIsIndeterminate(t *testing.T) {
	w := newWorld(t)
	h := w.h
	for i := 0; i < 50; i++ {
		h.advance(2 * time.Second)
		h.send(w.p, h.pub(i))
		h.send(w.b, h.rcv(i, "live"))
		if i == 20 {
			h.skipSeq(w.a, 50)
		}
		if i < 20 || i > 30 {
			h.send(w.a, h.rcv(i, "live"))
		}
		if i%5 == 0 {
			h.send(w.a, h.hb())
			h.send(w.b, h.hb())
			h.send(w.p, h.hb())
		}
	}
	h.settle(w.p, w.a, w.b)
	h.expect("lossmon_lost_total", 0)
	h.expect("lossmon_indeterminate_total", 11, "reason", "receiver_gap")
	h.expect("lossmon_coverage_gaps_total", 1, "reason", "receiver_gap")
}

func TestSEC004_FirstRecordAfterRegistrationInfersNoGap(t *testing.T) {
	w := newWorld(t)
	h := w.h
	s := h.register(20, regOpts{client: 20, role: "receive"})
	h.seq[s] = 499
	h.send(s, h.hb())
	h.skipSeq(s, 1)
	h.advance(time.Second)
	h.send(s, h.hb())
	h.expect("lossmon_coverage_gaps_total", 1, "reason", "receiver_gap")
}

// SEC-007: over-budget receipts are rejected and blind only the affected window.
func TestSEC007_OverBudgetIsIndeterminate(t *testing.T) {
	w := newWorld(t, func(c *config.Config) { c.ReceiptBurst = 5 })
	h := w.h
	h.advance(time.Second)
	h.send(w.p, h.pub(0))
	h.send(w.b, h.rcv(0, "live"))
	var flood []Record
	for i := 0; i < 400; i++ {
		flood = append(flood, h.rcv(5000+i, "live"))
	}
	h.send(w.a, flood...)
	h.send(w.a, h.rcv(0, "live"))
	h.settle(w.p, w.a, w.b)
	h.expect("lossmon_lost_total", 0)
	h.expect("lossmon_ingest_records_total", 306, "result", "over_budget", "type", "receive")
	h.expect("lossmon_indeterminate_total", 1, "reason", "rejected")
}

func TestSEC007_PublishBudgetPerMinute(t *testing.T) {
	w := newWorld(t)
	h := w.h
	h.advance(time.Second)
	var recs []Record
	for i := 0; i < 130; i++ {
		recs = append(recs, h.pub(i))
	}
	h.send(w.p, recs...)
	h.expect("lossmon_published_total", 100)
	h.expect("lossmon_ingest_records_total", 30, "result", "over_budget")
}

func TestSEC007_BudgetsUseEventTimeNotArrivalTime(t *testing.T) {
	w := newWorld(t)
	h := w.h
	h.beats(10*time.Minute, w.p, w.a, w.b)
	var recs []Record
	for m := 0; m < 3; m++ {
		for i := 0; i < 60; i++ {
			r := h.pub(m*100 + i)
			r.AtMs = h.now - 5*minuteMs + int64(m)*minuteMs + int64(i)*500
			r.SendAtMs = r.AtMs
			recs = append(recs, r)
		}
	}
	h.send(w.p, recs...)
	h.expect("lossmon_published_total", 180)
}

func TestSEC009_InvalidRecordsOpenRejectedGap(t *testing.T) {
	bad := map[string]func(h *harness, r *Record){
		"enum":            func(h *harness, r *Record) { r.Source = "nope" },
		"probe_id":        func(h *harness, r *Record) { r.ProbeID = "0xZZ" },
		"hash":            func(h *harness, r *Record) { r.Hash = "0x1" },
		"topic":           func(h *harness, r *Record) { r.Topic = "/other" },
		"future":          func(h *harness, r *Record) { r.AtMs += 10 * minuteMs },
		"huge":            func(h *harness, r *Record) { r.AtMs = 1 << 62 },
		"negative":        func(h *harness, r *Record) { r.AtMs = -5 },
		"msg_ts_far":      func(h *harness, r *Record) { r.MsgTsMs = h.now - 5*60*minuteMs },
		"msg_ts_overflow": func(h *harness, r *Record) { r.MsgTsMs = 1<<62 + 5 },
	}
	for name, mutate := range bad {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			h := w.h
			h.advance(time.Second)
			h.send(w.p, h.pub(0))
			h.send(w.b, h.rcv(0, "live"))
			r := h.rcv(0, "live")
			mutate(h, &r)
			h.send(w.a, r)
			h.settle(w.p, w.a, w.b)
			h.expect("lossmon_lost_total", 0)
			h.expect("lossmon_ingest_records_total", 1, "result", "invalid")
			h.expect("lossmon_indeterminate_total", 1, "reason", "rejected")
		})
	}
}

func TestSEC009_HeartbeatAndPublishValidation(t *testing.T) {
	w := newWorld(t)
	h := w.h
	h.advance(time.Second)
	hb := h.hb("/not/mine")
	pb := h.pub(0)
	pb.SendAtMs = pb.AtMs + 1
	conn := h.hb()
	conn.Conn = "weird"
	neg := h.hb()
	neg.DroppedTotal = -1
	h.send(w.p, hb, pb, conn, neg)
	h.expect("lossmon_ingest_records_total", 4, "result", "invalid")
	h.expect("lossmon_published_total", 0)
}

func TestSEC009_RoleEnforced(t *testing.T) {
	w := newWorld(t)
	h := w.h
	h.advance(time.Second)
	h.send(w.a, h.pub(0))
	h.send(w.p, h.rcv(0, "live"))
	h.expect("lossmon_ingest_records_total", 2, "result", "invalid")
}

// SEC-001/002: replayed or forged receipts can hide loss from at most that receiver, never create it.
func TestReplayBeforeGenuineIsDelivered(t *testing.T) {
	w := newWorld(t)
	h := w.h
	h.advance(time.Second)
	replay := h.rcv(0, "live")
	replay.Hash = hash(900)
	h.send(w.a, replay)
	h.send(w.p, h.pub(0))
	h.send(w.a, h.rcv(0, "live"))
	h.send(w.b, h.rcv(0, "live"))
	h.settle(w.p, w.a, w.b)
	h.expect("lossmon_delivered_total", 2)
	h.expect("lossmon_lost_total", 0)
	h.expect("lossmon_replayed_total", 1)
}

func TestReplayOverflowWithoutGenuineIsIndeterminate(t *testing.T) {
	w := newWorld(t)
	h := w.h
	h.advance(time.Second)
	h.send(w.p, h.pub(0))
	var recs []Record
	for i := 0; i < 4; i++ {
		r := h.rcv(0, "live")
		r.Hash = hash(900 + i)
		recs = append(recs, r)
	}
	over := h.rcv(0, "live")
	over.Hash = hash(950)
	over.Overflow = true
	recs = append(recs, over)
	h.send(w.a, recs...)
	h.send(w.b, h.rcv(0, "live"))
	h.settle(w.p, w.a, w.b)
	h.expect("lossmon_lost_total", 0)
	h.expect("lossmon_indeterminate_total", 1, "reason", "replayed")
	h.expect("lossmon_delivered_total", 1)
}

func TestReplayedHashNeverCountsAsDelivered(t *testing.T) {
	w := newWorld(t)
	h := w.h
	h.advance(time.Second)
	h.send(w.p, h.pub(0))
	replay := h.rcv(0, "live")
	replay.Hash = hash(900)
	h.send(w.a, replay)
	h.send(w.b, h.rcv(0, "live"))
	h.settle(w.p, w.a, w.b)
	h.expect("lossmon_delivered_total", 1)
	h.expect("lossmon_replayed_total", 1)
}

func TestPublishWithSameProbeAndOtherHashIsInvalid(t *testing.T) {
	w := newWorld(t)
	h := w.h
	h.advance(time.Second)
	h.send(w.p, h.pub(0))
	dup := h.pub(0)
	dup.Hash = hash(77)
	h.send(w.p, dup)
	h.expect("lossmon_ingest_records_total", 1, "result", "invalid")
	h.expect("lossmon_published_total", 1)
}

// Live sessions are never evicted to make room, and idle sessions expire instead.
func TestSessionsNeverEvictedAndIdleExpires(t *testing.T) {
	h := newHarness(t, func(c *config.Config) { c.MaxSessionsPerToken = 3; c.MaxSessions = 4 })
	for i := 1; i <= 3; i++ {
		h.register(i, regOpts{client: i, role: "receive"})
	}
	_, err := h.tryRegister(4, regOpts{client: 4, role: "receive"})
	if err == nil || err.Code != "token_session_limit" || err.Status != 429 || err.RetryAfterMs != 60000 {
		t.Fatalf("want token_session_limit 429, got %+v", err)
	}
	h.register(5, regOpts{client: 5, role: "receive", token: "other"})
	_, err = h.tryRegister(6, regOpts{client: 6, role: "receive", token: "other"})
	if err == nil || err.Code != "session_limit" {
		t.Fatalf("want session_limit, got %+v", err)
	}
	for i := 1; i <= 3; i++ {
		if h.c.sessions[uid("session", i)] == nil {
			t.Fatalf("session %d was evicted", i)
		}
	}
	h.advance(h.cfg.SessionIdleTimeout + time.Second)
	if _, err := h.trySend(uid("session", 1), h.hb()); err == nil || err.Code != "unknown_session" {
		t.Fatalf("want unknown_session after idle expiry, got %v", err)
	}
	h.register(6, regOpts{client: 6, role: "receive", token: "other"})
}

// SEC-008: the records/s limit is enforced per token and the batch is not applied.
func TestSEC008_RecordRateLimit(t *testing.T) {
	w := newWorld(t, func(c *config.Config) { c.TokenRecordsPerSec = 10 })
	h := w.h
	var recs []Record
	for i := 0; i < 60; i++ {
		recs = append(recs, h.hb())
	}
	before := h.seq[w.a]
	if _, err := h.trySend(w.a, recs...); err != nil {
		t.Fatalf("first batch: %v", err)
	}
	var recs2 []Record
	for i := 0; i < 60; i++ {
		recs2 = append(recs2, h.hb())
	}
	big := make([]Record, 0, 1000)
	for i := 0; i < 1000; i++ {
		big = append(big, h.hb())
	}
	_ = recs2
	_, err := h.trySend(w.a, big...)
	if err == nil || err.Code != "record_rate_limited" || err.RetryAfterMs <= 0 {
		t.Fatalf("want record_rate_limited, got %+v", err)
	}
	if h.c.sessions[w.a].lastSeq != before+60 {
		t.Fatalf("a rejected batch was applied")
	}
	h.seq[w.a] = before + 60
	h.now += err.RetryAfterMs
	if _, err := h.trySend(w.a, h.hb()); err != nil {
		t.Fatalf("after waiting: %v", err)
	}
}

// Label values come from allowlists, so random client strings cannot grow the series set.
func TestSEC_RandomStringsNeverReachLabels(t *testing.T) {
	h := newHarness(t, func(c *config.Config) { c.MaxSessionsPerToken = 5000; c.MaxSessions = 5000; c.MaxRateTotal = 1e9 })
	rng := rand.New(rand.NewSource(1))
	rnd := func() string { return fmt.Sprintf("RND-%x", rng.Int63()) }
	valid := h.register(1, regOpts{client: 1, role: "both"})
	h.send(valid, h.hb())
	fuzz := func(rounds int) {
		for i := 0; i < rounds; i++ {
			h.now += 1000
			req := regOpts{client: 100 + i, role: "both"}.request(h.now)
			req.Location = rnd()
			req.AppVersion, req.LogosDeliveryVersion = rnd()[:10], rnd()[:10]
			if i%3 == 0 {
				req.Mode, req.Role, req.AnonymityLevel = rnd(), rnd(), rnd()
			}
			if i%5 == 0 {
				req.Topics = []string{rnd()}
			}
			_, _ = h.c.Register(h.now, "op", uid("session", 100+i), req)
			r := Record{
				Type: []string{typeHeartbeat, typePublish, typeReceive, rnd()}[i%4], AtMs: h.now,
				Conn: rnd(), Subscribed: []string{rnd()}, ProbeID: rnd(), Hash: rnd(), Topic: rnd(), Outcome: rnd(),
				Source: rnd(), MsgTsMs: h.now,
			}
			_, _ = h.trySend(valid, r)
			h.send(valid, h.hb())
			sid := uid("session", 100+i)
			if h.c.sessions[sid] != nil {
				r.Topic = topicA
				_, _ = h.trySend(sid, r)
			}
		}
		h.c.Tick(h.now)
	}
	fuzz(300)
	first := h.seriesCount()
	fuzz(1200)
	second := h.seriesCount()
	if second != first {
		t.Errorf("series count grew from %d to %d under random input", first, second)
	}
	for _, f := range h.families() {
		for _, mt := range f.GetMetric() {
			for _, l := range mt.GetLabel() {
				if strings.Contains(l.GetValue(), "RND") {
					t.Fatalf("random string leaked into %s{%s=%q}", f.GetName(), l.GetName(), l.GetValue())
				}
			}
		}
	}
}
