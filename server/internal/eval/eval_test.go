package eval

import (
	"testing"
	"time"

	"github.com/logos-messaging/logos-delivery-loss-monitor/server/internal/config"
)

// CS-002: loss is measured per receiver, not globally.
func TestCS002_PerReceiverPairs(t *testing.T) {
	w := newWorld(t)
	h := w.h
	for i := 0; i < 100; i++ {
		h.advance(time.Second)
		recs := []Record{h.pub(i)}
		h.send(w.p, recs...)
		h.send(w.a, h.rcv(i, "live"))
		if i%10 != 0 {
			h.send(w.b, h.rcv(i, "live"))
		}
		if i%10 == 0 {
			h.send(w.p, h.hb())
			h.send(w.a, h.hb())
			h.send(w.b, h.hb())
		}
	}
	h.settle(w.p, w.a, w.b)
	h.expect("lossmon_expected_total", 200)
	h.expect("lossmon_delivered_total", 190)
	h.expect("lossmon_lost_total", 10)
	h.expect("lossmon_lost_total", 10, "receiver_mode", "edge", "publisher_mode", "core", "receiver_location", "as", "publisher_location", "eu")
	h.expect("lossmon_published_total", 100)
	h.expect("lossmon_indeterminate_total", 0)
}

func TestCS002_AllDelivered(t *testing.T) {
	w := newWorld(t)
	h := w.h
	for i := 0; i < 20; i++ {
		h.advance(time.Second)
		h.send(w.p, h.pub(i))
		h.send(w.a, h.rcv(i, "live"))
		h.send(w.b, h.rcv(i, "live"))
	}
	h.settle(w.p, w.a, w.b)
	h.expect("lossmon_expected_total", 40)
	h.expect("lossmon_delivered_total", 40, "source", "live")
	h.expect("lossmon_lost_total", 0)
	h.expect("lossmon_message_loss_ratio", 0)
	if n := h.sum("lossmon_latency_seconds"); n != 40 {
		t.Errorf("latency samples = %v, want 40", n)
	}
}

// CS-005: backfilled receipts inside the window are delivered; later ones are late, never retracting lost.
func TestCS005_HistoryAndLate(t *testing.T) {
	w := newWorld(t)
	h := w.h
	h.advance(time.Second)
	h.send(w.p, h.pub(0), h.pub(1))
	h.beats(5*time.Minute, w.p, w.a, w.b)
	h.send(w.a, h.rcv(0, "history"))
	h.send(w.b, h.rcv(0, "live"))
	h.send(w.b, h.rcv(1, "live"))
	h.settle(w.p, w.a, w.b)
	h.expect("lossmon_delivered_total", 1, "source", "history")
	h.expect("lossmon_lost_total", 1, "receiver_location", "na")
	h.expect("lossmon_late_total", 0)

	h.send(w.a, h.rcv(1, "history"))
	h.expect("lossmon_late_total", 1)
	h.expect("lossmon_lost_total", 1)
}

func TestCS005_ReceiptAfterWindowBeforeDecisionIsLate(t *testing.T) {
	w := newWorld(t)
	h := w.h
	h.advance(time.Second)
	h.send(w.p, h.pub(0))
	h.send(w.b, h.rcv(0, "live"))
	h.beats(h.cfg.Window+5*time.Second, w.p, w.a, w.b)
	h.send(w.a, h.rcv(0, "history"))
	h.settle(w.p, w.a, w.b)
	h.expect("lossmon_lost_total", 1, "receiver_location", "na")
	h.expect("lossmon_late_total", 1)
	h.expect("lossmon_delivered_total", 1)
}

// CS-006: repeated receipts are duplicates, not extra deliveries.
func TestCS006_Duplicates(t *testing.T) {
	w := newWorld(t)
	h := w.h
	h.advance(time.Second)
	h.send(w.p, h.pub(0))
	h.send(w.a, h.rcv(0, "live"))
	h.send(w.a, h.rcv(0, "history"))
	h.send(w.b, h.rcv(0, "live"))
	h.settle(w.p, w.a, w.b)
	h.expect("lossmon_delivered_total", 2)
	h.expect("lossmon_duplicate_total", 1)
	h.send(w.a, h.rcv(0, "history"))
	h.expect("lossmon_duplicate_total", 2)
	h.expect("lossmon_delivered_total", 2)
}

// A retried batch (same seq) is not a network duplicate.
func TestRetriedBatchIsIdempotent(t *testing.T) {
	w := newWorld(t)
	h := w.h
	h.advance(time.Second)
	h.send(w.p, h.pub(0))
	rec := h.rcv(0, "live")
	h.send(w.a, rec)
	retry := rec
	retry.Seq = h.seq[w.a]
	if _, err := h.c.Ingest(h.now, "op", w.a, &Batch{SentAtMs: h.now, Records: []Record{retry}}); err != nil {
		t.Fatal(err)
	}
	h.send(w.b, h.rcv(0, "live"))
	h.settle(w.p, w.a, w.b)
	h.expect("lossmon_duplicate_total", 0)
	h.expect("lossmon_delivered_total", 2)
	h.expect("lossmon_ingest_records_total", 1, "result", "duplicate_seq")
}

// CS-007: a stopped receiver is never charged as loss.
func TestCS007_EndedReceiverIsIndeterminate(t *testing.T) {
	w := newWorld(t)
	h := w.h
	h.advance(time.Second)
	h.send(w.p, h.pub(0))
	h.send(w.b, h.rcv(0, "live"))
	h.beats(3*time.Minute, w.p, w.a, w.b)
	h.send(w.a, h.end())
	h.settle(w.p, w.b)
	h.expect("lossmon_lost_total", 0)
	h.expect("lossmon_indeterminate_total", 1, "reason", "receiver_ended", "receiver_location", "na")
	h.expect("lossmon_delivered_total", 1)
}

func TestCS007_SilentReceiverIsIndeterminate(t *testing.T) {
	w := newWorld(t)
	h := w.h
	h.advance(time.Second)
	h.send(w.p, h.pub(0))
	h.send(w.b, h.rcv(0, "live"))
	h.beats(5*time.Minute, w.p, w.a, w.b)
	h.beats(h.cfg.Window+h.cfg.Grace+h.cfg.Hold+time.Minute, w.p, w.b)
	h.expect("lossmon_lost_total", 0)
	h.expect("lossmon_indeterminate_total", 1, "receiver_location", "na")
	h.expect("lossmon_expected_total", 1)
}

func TestCS007_LaggingReceiverDefersThenIndeterminate(t *testing.T) {
	w := newWorld(t, func(c *config.Config) { c.SessionIdleTimeout = 6 * time.Hour })
	h := w.h
	h.advance(time.Second)
	h.send(w.p, h.pub(0))
	h.send(w.b, h.rcv(0, "live"))
	h.beats(h.cfg.Window+h.cfg.Grace+time.Minute, w.p, w.b)
	h.expect("lossmon_lost_total", 0)
	h.expect("lossmon_indeterminate_total", 0)
	h.send(w.a, h.hb())
	h.beats(h.cfg.Hold, w.p, w.b)
	h.expect("lossmon_lost_total", 0)
	h.expect("lossmon_indeterminate_total", 1, "receiver_location", "na")
}

func TestHeartbeatGapMakesWindowIndeterminate(t *testing.T) {
	w := newWorld(t)
	h := w.h
	h.advance(time.Second)
	h.send(w.p, h.pub(0))
	h.send(w.b, h.rcv(0, "live"))
	h.beats(2*time.Minute, w.p, w.b)
	h.advance(2 * time.Minute)
	h.send(w.a, h.hb())
	h.settle(w.p, w.a, w.b)
	h.expect("lossmon_lost_total", 0)
	h.expect("lossmon_indeterminate_total", 1, "reason", "receiver_gap")
}

func TestDeliveredBeatsGap(t *testing.T) {
	w := newWorld(t)
	h := w.h
	h.advance(time.Second)
	h.send(w.p, h.pub(0))
	h.send(w.a, h.rcv(0, "live"))
	h.send(w.b, h.rcv(0, "live"))
	h.skipSeq(w.a, 5)
	h.settle(w.p, w.a, w.b)
	h.expect("lossmon_delivered_total", 2)
	h.expect("lossmon_indeterminate_total", 0)
}

// CS-008: a restarted client re-registers; the old session's publishes are still evaluated.
func TestCS008_ClientRestart(t *testing.T) {
	w := newWorld(t)
	h := w.h
	h.advance(time.Second)
	h.send(w.p, h.pub(0))
	h.send(w.a, h.rcv(0, "live"))
	h.send(w.b, h.rcv(0, "live"))
	h.send(w.p, h.end())
	p2 := h.register(10, regOpts{client: 1, role: "both"})
	h.send(p2, h.hb())
	h.beats(40*time.Second, p2, w.a, w.b)
	h.send(p2, h.rcv(0, "live"))
	h.settle(p2, w.a, w.b)
	h.expect("lossmon_expected_total", 2)
	h.expect("lossmon_delivered_total", 2)
	h.expect("lossmon_lost_total", 0)
}

// R18: after a server restart nothing published earlier is counted, and clients re-register.
func TestServerRestartNeverCreatesLoss(t *testing.T) {
	w := newWorld(t)
	h := w.h
	h.advance(time.Second)
	pubAt := h.now
	h.send(w.p, h.pub(0))

	h.c = NewCore(h.cfg, h.m, nil, keyID, h.now+minuteMs)
	h.advance(time.Minute)
	if _, err := h.trySend(w.a, h.hb()); err == nil || err.Code != "unknown_session" {
		t.Fatalf("want unknown_session, got %v", err)
	}
	a := h.register(2, regOpts{client: 2, role: "receive", loc: "na"})
	b := h.register(3, regOpts{client: 3, role: "receive", mode: "edge", loc: "as"})
	p := h.register(1, regOpts{client: 1, role: "publish"})
	h.seq[a], h.seq[b], h.seq[p] = 0, 0, 0
	old := h.pub(0)
	old.AtMs, old.SendAtMs = pubAt, pubAt
	h.send(p, old)
	late := h.rcv(0, "live")
	late.AtMs = pubAt + 1000
	h.send(a, late)
	h.send(a, h.hb())
	h.send(b, h.hb())
	h.settle(p, a, b)
	h.expect("lossmon_lost_total", 0)
	h.expect("lossmon_expected_total", 0)
	h.expect("lossmon_skipped_total", 2, "reason", "before_registration")
}

// Orphan ordering: a receipt may reach the server before the publish report.
func TestReceiptBeforePublish(t *testing.T) {
	w := newWorld(t)
	h := w.h
	h.advance(time.Second)
	h.send(w.a, h.rcv(0, "live"))
	h.send(w.b, h.rcv(0, "live"))
	h.advance(2 * time.Second)
	h.send(w.p, h.pub(0))
	h.settle(w.p, w.a, w.b)
	h.expect("lossmon_delivered_total", 2)
	h.expect("lossmon_lost_total", 0)
	h.expect("lossmon_unmatched_total", 0)
}

func TestOrphanExpiresAsUnmatchedAndLatePublishIsSkipped(t *testing.T) {
	w := newWorld(t)
	h := w.h
	h.c.startMs = h.now - 2*time.Hour.Milliseconds()
	h.advance(time.Second)
	h.send(w.a, h.rcv(0, "live"))
	h.beats(h.cfg.Window+h.cfg.Grace+h.cfg.Hold+time.Minute, w.p, w.a, w.b)
	h.expect("lossmon_unmatched_total", 1, "content_topic", topicA)
	pub := h.pub(0)
	pub.AtMs, pub.SendAtMs = h.now-1000, h.now-1000
	h.send(w.p, pub)
	h.settle(w.p, w.a, w.b)
	h.expect("lossmon_skipped_total", 1, "reason", "publish_report_late")
	h.expect("lossmon_lost_total", 0)
	h.expect("lossmon_expected_total", 0)
}

func TestOrphansBeforeServerStartAreSkipped(t *testing.T) {
	w := newWorld(t)
	h := w.h
	h.c.startMs = h.now
	h.send(w.a, h.rcv(0, "live"))
	h.beats(h.cfg.Window+h.cfg.Grace+h.cfg.Hold+time.Minute, w.p, w.a, w.b)
	h.expect("lossmon_skipped_total", 1, "reason", "before_start")
	h.expect("lossmon_unmatched_total", 0)
}

// CS-013: send failures stay out of the loss denominator.
func TestCS013_SendFailedNotExpected(t *testing.T) {
	w := newWorld(t)
	h := w.h
	h.advance(time.Second)
	bad := h.pub(0)
	bad.Outcome = "error"
	bad.Queued = true
	timeout := h.pub(1)
	timeout.Outcome = "timeout"
	h.send(w.p, bad, timeout)
	h.settle(w.p, w.a, w.b)
	h.expect("lossmon_send_failed_total", 1, "outcome", "error")
	h.expect("lossmon_send_failed_total", 1, "outcome", "timeout")
	h.expect("lossmon_send_queued_total", 1)
	h.expect("lossmon_expected_total", 0)
	h.expect("lossmon_published_total", 0)
}

// CS-011: a server or uplink outage does not turn into loss; reports keep their event times.
func TestCS011_BufferedReportsAfterOutage(t *testing.T) {
	w := newWorld(t)
	h := w.h
	type batch struct {
		sid  string
		recs []Record
	}
	var held []batch
	for i := 0; i < 30; i++ {
		h.now += 2000
		held = append(held,
			batch{w.p, []Record{h.pub(i)}},
			batch{w.a, []Record{h.rcv(i, "live")}},
			batch{w.b, []Record{h.rcv(i, "live")}})
		if i%5 == 0 {
			held = append(held, batch{w.a, []Record{h.hb()}}, batch{w.b, []Record{h.hb()}}, batch{w.p, []Record{h.hb()}})
		}
	}
	h.now += 10_000
	h.c.Tick(h.now)
	for _, b := range held {
		h.send(b.sid, b.recs...)
	}
	h.settle(w.p, w.a, w.b)
	h.expect("lossmon_lost_total", 0)
	h.expect("lossmon_delivered_total", 60)
}

func TestSkewedReceiverIsIndeterminate(t *testing.T) {
	w := newWorld(t, func(c *config.Config) { c.MaxClockOffset = 5 * time.Second; c.SessionIdleTimeout = 6 * time.Hour })
	h := w.h
	h.advance(time.Second)
	h.send(w.p, h.pub(0))
	h.send(w.b, h.rcv(0, "live"))
	h.now += 1000
	skewedBatch := Batch{SentAtMs: h.now - 20_000, Records: []Record{{Seq: h.seq[w.a] + 1, Type: typeHeartbeat, AtMs: h.now - 20_000, Conn: "connected"}}}
	for i := 0; i < 15; i++ {
		skewedBatch.Records[0].Seq = h.seq[w.a] + 1
		h.seq[w.a]++
		if _, err := h.c.Ingest(h.now, "op", w.a, &skewedBatch); err != nil {
			t.Fatal(err)
		}
	}
	h.beats(h.cfg.Window+h.cfg.Grace+30*time.Second, w.p, w.b)
	h.expect("lossmon_lost_total", 0)
	h.expect("lossmon_indeterminate_total", 1, "reason", "clock_skew")
	h.expect("lossmon_sessions_clock_skewed", 1)
}

func TestLossRatioWindow(t *testing.T) {
	w := newWorld(t)
	h := w.h
	for i := 0; i < 4; i++ {
		h.advance(time.Second)
		h.send(w.p, h.pub(i))
		h.send(w.b, h.rcv(i, "live"))
		if i != 0 {
			h.send(w.a, h.rcv(i, "live"))
		}
	}
	h.settle(w.p, w.a, w.b)
	h.expect("lossmon_message_loss_ratio", 0.25, "receiver_location", "na")
	h.expect("lossmon_message_loss_ratio", 0, "receiver_location", "as")
	h.beats(h.cfg.LossRatioWindow+2*time.Minute, w.p, w.a, w.b)
	if got := h.sum("lossmon_message_loss_ratio"); got != 0 {
		t.Errorf("ratio series still present after window: %v", got)
	}
	h.expect("lossmon_lost_total", 1)
}

func TestSelfReceiptIsIgnored(t *testing.T) {
	w := newWorld(t)
	h := w.h
	pb := h.register(11, regOpts{client: 1, role: "both"})
	h.send(pb, h.hb())
	h.beats(40*time.Second, pb, w.a, w.b, w.p)
	h.advance(time.Second)
	h.send(w.p, h.pub(0))
	h.send(pb, h.rcv(0, "live"))
	h.send(w.a, h.rcv(0, "live"))
	h.send(w.b, h.rcv(0, "live"))
	h.settle(pb, w.p, w.a, w.b)
	h.expect("lossmon_expected_total", 2)
	h.expect("lossmon_duplicate_total", 0)
}
