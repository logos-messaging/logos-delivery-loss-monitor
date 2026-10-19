package stats

import (
	"fmt"
	"testing"
	"time"
)

const (
	t0  = int64(1_760_000_000_000)
	day = int64(24 * time.Hour / time.Millisecond)
)

var (
	pk = PairKey{Topic: "t", PubMode: "core", RecvMode: "edge", PubLoc: "eu", RecvLoc: "na", PubAnon: "none"}
	ck = ClientKey{PubToken: "p", PubClient: "c1", RecvToken: "r", RecvClient: "c2", Topic: "t"}
)

func delivered() Counts { return Counts{Expected: 1, DeliveredLive: 1} }
func lost() Counts      { return Counts{Expected: 1, Lost: 1} }

func TestBucketsAreKeyedByPublishTime(t *testing.T) {
	s := New(24*time.Hour, 100, t0-day)
	b := t0 - t0%BucketMs
	s.Add(b+1, pk, ck, delivered())
	s.Add(b+BucketMs-1, pk, ck, lost())
	s.Add(b+BucketMs, pk, ck, delivered())

	first := s.Query(t0, Query{From: b, To: b + BucketMs})
	if first.Totals.Expected != 2 || first.Totals.Lost != 1 || first.Totals.Delivered() != 1 {
		t.Fatalf("first bucket: %+v", first.Totals)
	}
	both := s.Query(t0, Query{From: b, To: b + 2*BucketMs})
	if both.Totals.Expected != 3 {
		t.Fatalf("both buckets: %+v", both.Totals)
	}
	if r, ok := both.Totals.LossRatio(); !ok || r != 1.0/3 {
		t.Fatalf("loss ratio %v %v", r, ok)
	}
}

func TestQueryRangeIsAlignedOutwards(t *testing.T) {
	s := New(24*time.Hour, 100, t0-day)
	b := t0 - t0%BucketMs
	s.Add(b+5, pk, ck, delivered())
	res := s.Query(t0, Query{From: b + 3*60_000, To: b + 4*60_000})
	if res.From != b || res.To != b+BucketMs {
		t.Fatalf("aligned range [%d,%d), want [%d,%d)", res.From, res.To, b, b+BucketMs)
	}
	if res.Totals.Expected != 1 {
		t.Fatalf("totals %+v", res.Totals)
	}
	empty := s.Query(t0, Query{From: b + BucketMs, To: b + 2*BucketMs})
	if empty.Totals.Expected != 0 {
		t.Fatalf("next bucket should be empty: %+v", empty.Totals)
	}
	if _, ok := empty.Totals.LossRatio(); ok {
		t.Fatal("loss ratio defined without decided pairs")
	}
}

func TestPartialFlag(t *testing.T) {
	start := t0 - 3*60*60_000
	s := New(2*time.Hour, 100, start)
	if s.Query(t0, Query{From: t0 - 60*60_000, To: t0}).Partial {
		t.Fatal("range inside retention and after start is complete")
	}
	if !s.Query(t0, Query{From: t0 - 3*60*60_000, To: t0}).Partial {
		t.Fatal("range beyond retention must be partial")
	}
	s = New(24*time.Hour, 100, t0-30*60_000)
	if !s.Query(t0, Query{From: t0 - 60*60_000, To: t0}).Partial {
		t.Fatal("range starting before the server must be partial")
	}
}

func TestTopicFilterAndPairBreakdown(t *testing.T) {
	s := New(24*time.Hour, 100, t0-day)
	other := pk
	other.Topic, other.RecvLoc = "u", "as"
	otherClient := ck
	otherClient.Topic = "u"
	s.Add(t0, pk, ck, delivered())
	s.Add(t0, other, otherClient, lost())

	all := s.Query(t0, Query{From: t0 - BucketMs, To: t0 + BucketMs, By: ByPair})
	if all.Totals.Expected != 2 || len(all.Pairs) != 2 {
		t.Fatalf("all: %+v pairs=%d", all.Totals, len(all.Pairs))
	}
	if all.Pairs[0].Key.Topic != "t" || all.Pairs[1].Key.Topic != "u" {
		t.Fatalf("pairs not sorted: %+v", all.Pairs)
	}
	one := s.Query(t0, Query{From: t0 - BucketMs, To: t0 + BucketMs, Topic: "u", By: ByPair})
	if one.Totals.Lost != 1 || one.Totals.Delivered() != 0 || len(one.Pairs) != 1 {
		t.Fatalf("topic filter: %+v", one)
	}
	if len(one.Clients) != 0 {
		t.Fatal("clients must only be listed for by=client")
	}
}

func TestIndeterminateReasonsAreSeparate(t *testing.T) {
	s := New(24*time.Hour, 100, t0-day)
	var d Counts
	d.Indeterminate[ReasonIndex("evicted")] = 2
	d.Indeterminate[ReasonIndex("replayed")] = 1
	s.Add(t0, pk, ck, d)
	res := s.Query(t0, Query{From: t0 - BucketMs, To: t0 + BucketMs})
	if res.Totals.IndeterminateTotal() != 3 || res.Totals.Indeterminate[ReasonIndex("evicted")] != 2 {
		t.Fatalf("totals %+v", res.Totals)
	}
	if res.Totals.Lost != 0 || res.Totals.Expected != 0 {
		t.Fatalf("indeterminate pairs must not count as decided: %+v", res.Totals)
	}
	if ReasonIndex("nope") != -1 {
		t.Fatal("unknown reason must not map to an index")
	}
}

func TestClientKeysAreCappedLeastRecentlyUsedFirst(t *testing.T) {
	s := New(24*time.Hour, 3, t0-day)
	client := func(i int) ClientKey { return ClientKey{PubClient: fmt.Sprintf("c%d", i), Topic: "t"} }
	for i := 0; i < 3; i++ {
		s.Add(t0, pk, client(i), delivered())
	}
	s.Add(t0, pk, client(0), delivered())
	s.Add(t0, pk, client(3), delivered())

	res := s.Query(t0, Query{From: t0 - BucketMs, To: t0 + BucketMs, By: ByClient})
	if len(res.Clients) != 3 {
		t.Fatalf("kept %d client keys, want 3", len(res.Clients))
	}
	kept := map[string]int64{}
	for _, c := range res.Clients {
		kept[c.Key.PubClient] = c.Counts.Expected
	}
	if _, ok := kept["c1"]; ok {
		t.Fatalf("c1 is the least recently used and must be evicted: %v", kept)
	}
	if kept["c0"] != 2 || kept["c2"] != 1 || kept["c3"] != 1 {
		t.Fatalf("kept %v", kept)
	}
	if !res.Partial {
		t.Fatal("a truncated breakdown must be partial")
	}
	if res.Totals.Expected != 5 {
		t.Fatalf("totals must not lose evicted keys: %+v", res.Totals)
	}
	pairs := s.Query(t0, Query{From: t0 - BucketMs, To: t0 + BucketMs, By: ByPair})
	if pairs.Partial {
		t.Fatal("client eviction must not make a pair query partial")
	}
}

func TestPruneDropsExpiredBucketsAndTheirClientKeys(t *testing.T) {
	s := New(time.Hour, 100, t0-day)
	old := t0 - 3*60*60_000
	s.Add(old, pk, ck, delivered())
	s.Add(t0, pk, ClientKey{PubClient: "new", Topic: "t"}, delivered())
	s.Prune(t0)
	if len(s.buckets) != 1 || s.lru.Len() != 1 {
		t.Fatalf("buckets=%d lru=%d, want 1 and 1", len(s.buckets), s.lru.Len())
	}
	res := s.Query(t0, Query{From: old - BucketMs, To: t0 + BucketMs, By: ByClient})
	if res.Totals.Expected != 1 || len(res.Clients) != 1 || res.Clients[0].Key.PubClient != "new" {
		t.Fatalf("after prune: %+v", res)
	}
}
