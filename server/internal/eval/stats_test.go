package eval

import (
	"testing"
	"time"

	"github.com/logos-messaging/logos-delivery-loss-monitor/server/internal/config"
	"github.com/logos-messaging/logos-delivery-loss-monitor/server/internal/metrics"
	"github.com/logos-messaging/logos-delivery-loss-monitor/server/internal/stats"
)

func (h *harness) stats(by string) stats.Result {
	return h.c.Stats(h.now, stats.Query{From: t0 - stats.BucketMs, To: h.now + stats.BucketMs, By: by})
}

func TestStatsReasonsMatchMetrics(t *testing.T) {
	got, want := stats.Reasons, metrics.Reasons()
	if len(got) != len(want) {
		t.Fatalf("stats.Reasons = %v, metrics = %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("stats.Reasons = %v, metrics = %v", got, want)
		}
	}
}

func TestStatsMatchPrometheusCounters(t *testing.T) {
	w := newWorld(t)
	h := w.h
	for i := 0; i < 60; i++ {
		h.advance(time.Second)
		h.send(w.p, h.pub(i))
		h.send(w.a, h.rcv(i, "live"))
		if i%10 != 0 {
			h.send(w.b, h.rcv(i, "history"))
		}
		if i%10 == 0 {
			h.send(w.p, h.hb())
			h.send(w.a, h.hb())
			h.send(w.b, h.hb())
		}
	}
	h.settle(w.p, w.a, w.b)
	h.send(w.b, h.rcv(0, "live"))
	h.send(w.a, h.rcv(1, "live"))
	wrong := h.rcv(2, "live")
	wrong.Hash = hash(999)
	h.send(w.a, wrong)

	res := h.stats("")
	tot := res.Totals
	checks := []struct {
		name string
		got  int64
		want string
	}{
		{"expected", tot.Expected, "lossmon_expected_total"},
		{"delivered", tot.Delivered(), "lossmon_delivered_total"},
		{"lost", tot.Lost, "lossmon_lost_total"},
		{"late", tot.Late, "lossmon_late_total"},
		{"duplicate", tot.Duplicate, "lossmon_duplicate_total"},
		{"replayed", tot.Replayed, "lossmon_replayed_total"},
		{"indeterminate", tot.IndeterminateTotal(), "lossmon_indeterminate_total"},
	}
	for _, c := range checks {
		if want := int64(h.sum(c.want)); c.got != want {
			t.Errorf("%s: stats %d, prometheus %d", c.name, c.got, want)
		}
	}
	if tot.Expected != 120 || tot.Lost != 6 || tot.Late != 1 || tot.Duplicate != 1 || tot.Replayed != 1 {
		t.Errorf("unexpected totals: %+v", tot)
	}
	if tot.DeliveredLive != 60 || tot.DeliveredHistory != 54 {
		t.Errorf("delivered by source: live %d history %d", tot.DeliveredLive, tot.DeliveredHistory)
	}
}

func TestStatsPairBreakdownSplitsReceivers(t *testing.T) {
	w := newWorld(t)
	h := w.h
	for i := 0; i < 20; i++ {
		h.advance(time.Second)
		h.send(w.p, h.pub(i))
		h.send(w.a, h.rcv(i, "live"))
	}
	h.settle(w.p, w.a, w.b)
	res := h.stats(stats.ByPair)
	if len(res.Pairs) != 2 {
		t.Fatalf("pairs: %+v", res.Pairs)
	}
	for _, p := range res.Pairs {
		switch p.Key.RecvLoc {
		case "na":
			if p.Counts.Delivered() != 20 || p.Counts.Lost != 0 || p.Key.RecvMode != "core" || p.Key.PubLoc != "eu" {
				t.Errorf("core/na pair: %+v", p)
			}
		case "as":
			if p.Counts.Lost != 20 || p.Counts.Delivered() != 0 || p.Key.RecvMode != "edge" {
				t.Errorf("edge/as pair: %+v", p)
			}
		default:
			t.Errorf("unexpected pair %+v", p.Key)
		}
	}
}

func TestStatsByClientCarriesTokenNames(t *testing.T) {
	h := newHarness(t)
	p := h.register(1, regOpts{token: "pub-token", client: 1, role: "publish"})
	r := h.register(2, regOpts{token: "recv-token", client: 2, role: "receive"})
	for _, s := range []string{p, r} {
		h.send(s, h.hb())
	}
	h.beats(40*time.Second, p, r)
	for i := 0; i < 5; i++ {
		h.advance(time.Second)
		h.send(p, h.pub(i))
		h.send(r, h.rcv(i, "live"))
	}
	h.settle(p, r)
	res := h.stats(stats.ByClient)
	if len(res.Clients) != 1 {
		t.Fatalf("clients: %+v", res.Clients)
	}
	k := res.Clients[0].Key
	if k.PubToken != "pub-token" || k.RecvToken != "recv-token" || k.PubClient != uid("client", 1) ||
		k.RecvClient != uid("client", 2) || k.Topic != topicA {
		t.Fatalf("client key: %+v", k)
	}
	if res.Clients[0].Counts.Delivered() != 5 {
		t.Fatalf("counts: %+v", res.Clients[0].Counts)
	}
}

// SEC-003 for the aggregates: eviction opens no lost pair there either.
func TestStatsEvictionNeverProducesLost(t *testing.T) {
	w := newWorld(t, func(c *config.Config) { c.MaxOrphans = 20; c.MaxPendingPublishes = 3 })
	h := w.h
	h.advance(time.Second)
	h.send(w.a, h.rcv(0, "live"))
	h.send(w.b, h.rcv(0, "live"))
	for i := 1; i <= 100; i++ {
		h.send(w.a, h.rcv(1000+i, "live"))
	}
	for i := 0; i < 10; i++ {
		h.advance(time.Second)
		h.send(w.p, h.pub(i))
	}
	h.settle(w.p, w.a, w.b)
	tot := h.stats("").Totals
	if tot.Lost != 0 {
		t.Fatalf("eviction produced lost pairs: %+v", tot)
	}
	evicted := tot.Indeterminate[stats.ReasonIndex(metrics.ReasonEvicted)]
	if evicted == 0 || evicted != int64(h.sum("lossmon_indeterminate_total", "reason", "evicted")) {
		t.Fatalf("evicted: stats %d, prometheus %v", evicted, h.sum("lossmon_indeterminate_total", "reason", "evicted"))
	}
}

func TestStatsAreRetainedOnlyForTheRetentionPeriod(t *testing.T) {
	w := newWorld(t, func(c *config.Config) { c.StatsRetention = time.Hour })
	h := w.h
	h.advance(time.Second)
	h.send(w.p, h.pub(0))
	h.send(w.a, h.rcv(0, "live"))
	h.send(w.b, h.rcv(0, "live"))
	h.settle(w.p, w.a, w.b)
	if h.stats("").Totals.Expected != 2 {
		t.Fatal("pairs missing right after they were decided")
	}
	h.beats(2*time.Hour, w.p, w.a, w.b)
	if got := h.stats("").Totals.Expected; got != 0 {
		t.Fatalf("expected the bucket to be pruned, got %d", got)
	}
}
