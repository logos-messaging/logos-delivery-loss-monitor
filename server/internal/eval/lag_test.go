package eval

import (
	"testing"
	"time"

	"github.com/logos-messaging/logos-delivery-loss-monitor/server/internal/config"
)

const outcomeLag = 40 * time.Second

// A receipt can precede the publisher's outcome event by more than G; dropping it must still never yield lost.
func TestOutcomeLag_OrphanEvictionNeverProducesLost(t *testing.T) {
	for name, fillers := range map[string]int{"blind_set": 25, "blind_set_overflow": 100} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, func(c *config.Config) { c.MaxOrphans = 20 })
			h := w.h
			h.advance(time.Second)
			h.send(w.a, h.rcv(0, "live"))
			h.send(w.b, h.rcv(0, "live"))
			for i := 1; i <= fillers; i++ {
				h.send(w.a, h.rcv(1000+i, "live"))
			}
			h.beats(outcomeLag, w.p, w.a, w.b)
			h.send(w.p, h.pub(0))
			h.settle(w.p, w.a, w.b)
			h.expect("lossmon_lost_total", 0)
			h.expect("lossmon_indeterminate_total", 2, "reason", "evicted")
		})
	}
}

func TestOutcomeLag_OverBudgetNeverProducesLost(t *testing.T) {
	w := newWorld(t, func(c *config.Config) { c.ReceiptBurst = 5 })
	h := w.h
	h.advance(time.Second)
	var flood []Record
	for i := 0; i < 400; i++ {
		flood = append(flood, h.rcv(5000+i, "live"))
	}
	h.send(w.a, flood...)
	h.send(w.a, h.rcv(0, "live"))
	h.send(w.b, h.rcv(0, "live"))
	h.beats(outcomeLag, w.p, w.a, w.b)
	h.send(w.p, h.pub(0))
	h.settle(w.p, w.a, w.b)
	h.expect("lossmon_lost_total", 0)
	h.expect("lossmon_delivered_total", 1)
	h.expect("lossmon_indeterminate_total", 1, "reason", "rejected")
}

func TestOutcomeLag_InvalidReceiptNeverProducesLost(t *testing.T) {
	w := newWorld(t)
	h := w.h
	h.advance(time.Second)
	bad := h.rcv(0, "live")
	bad.MsgTsMs = h.now - 5*60*minuteMs
	h.send(w.a, bad)
	h.send(w.b, h.rcv(0, "live"))
	h.beats(outcomeLag, w.p, w.a, w.b)
	h.send(w.p, h.pub(0))
	h.settle(w.p, w.a, w.b)
	h.expect("lossmon_lost_total", 0)
	h.expect("lossmon_delivered_total", 1)
	h.expect("lossmon_indeterminate_total", 1, "reason", "rejected")
}

func TestOutcomeLag_ReceiptWithoutProbeIDNeverProducesLost(t *testing.T) {
	w := newWorld(t)
	h := w.h
	h.advance(time.Second)
	bad := h.rcv(0, "live")
	bad.ProbeID = "0xZZ"
	h.send(w.a, bad)
	h.send(w.b, h.rcv(0, "live"))
	h.beats(outcomeLag, w.p, w.a, w.b)
	h.send(w.p, h.pub(0))
	h.settle(w.p, w.a, w.b)
	h.expect("lossmon_lost_total", 0)
	h.expect("lossmon_indeterminate_total", 1, "reason", "rejected")
}

func TestOutcomeLag_SeqDropNeverProducesLost(t *testing.T) {
	w := newWorld(t)
	h := w.h
	h.advance(time.Second)
	h.skipSeq(w.a, 1)
	h.send(w.a, h.hb())
	h.send(w.b, h.rcv(0, "live"))
	h.beats(outcomeLag, w.p, w.a, w.b)
	h.send(w.p, h.pub(0))
	h.settle(w.p, w.a, w.b)
	h.expect("lossmon_lost_total", 0)
	h.expect("lossmon_delivered_total", 1)
	h.expect("lossmon_indeterminate_total", 1, "reason", "receiver_gap")
}

func TestOutcomeLag_HealthyReceiverStillLoses(t *testing.T) {
	w := newWorld(t)
	h := w.h
	h.advance(time.Second)
	h.send(w.b, h.rcv(0, "live"))
	h.beats(outcomeLag, w.p, w.a, w.b)
	h.send(w.p, h.pub(0))
	h.settle(w.p, w.a, w.b)
	h.expect("lossmon_lost_total", 1)
	h.expect("lossmon_delivered_total", 1)
}

func TestBlindSet_BoundedAndExpires(t *testing.T) {
	b := newBlindSet()
	s := &session{uid: 1}
	if out := b.add(s, "p1", "rejected", 10, 100, 2); len(out) != 0 {
		t.Fatalf("unexpected push-out %v", out)
	}
	b.add(s, "p1", "evicted", 10, 100, 2)
	if got := b.reason("p1", 1); got != "evicted" {
		t.Fatalf("reason = %q, want the stronger one", got)
	}
	b.add(s, "p2", "rejected", 20, 110, 2)
	out := b.add(s, "p3", "rejected", 30, 120, 2)
	if len(out) != 1 || out[0].at != 10 || b.reason("p1", 1) != "" {
		t.Fatalf("oldest entry was not pushed out: %v", out)
	}
	b.prune(1000, 500)
	if len(b.entries) != 0 || len(b.fifo) != 0 {
		t.Fatalf("expired entries remain: %d", len(b.entries))
	}
}

func TestNegativeSeqOpensRejectedGap(t *testing.T) {
	w := newWorld(t)
	h := w.h
	h.advance(time.Second)
	neg := h.rcv(0, "live")
	if _, err := h.c.Ingest(h.now, "op", w.a, &Batch{SentAtMs: h.now, Records: []Record{neg}}); err != nil {
		t.Fatal(err)
	}
	neg.Seq = -1
	if _, err := h.c.Ingest(h.now, "op", w.a, &Batch{SentAtMs: h.now, Records: []Record{neg}}); err != nil {
		t.Fatal(err)
	}
	h.expect("lossmon_coverage_gaps_total", 1, "reason", "rejected")
}

func TestClockOffsetUsesArrivalTime(t *testing.T) {
	w := newWorld(t, func(c *config.Config) { c.SessionIdleTimeout = 6 * time.Hour })
	h := w.h
	for i := 0; i < 20; i++ {
		sent := h.now
		h.now += 2 * minuteMs
		h.seq[w.a]++
		b := Batch{SentAtMs: sent, ArrivedMs: sent, Records: []Record{{Seq: h.seq[w.a], Type: typeHeartbeat, AtMs: sent, Conn: "connected"}}}
		if _, err := h.c.Ingest(h.now, "op", w.a, &b); err != nil {
			t.Fatal(err)
		}
	}
	h.c.Tick(h.now)
	h.expect("lossmon_sessions_clock_skewed", 0)
}
