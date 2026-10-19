// Package stats keeps loss aggregates in 10-minute buckets keyed by the publish time t_P and answers
// range queries over them. A Store is not safe for concurrent use: the evaluation goroutine owns it.
package stats

import (
	"container/list"
	"sort"
	"time"
)

// BucketMs is the bucket width.
const BucketMs = int64(10 * time.Minute / time.Millisecond)

// DefaultMaxClientKeys caps the per-client entries kept across all buckets.
const DefaultMaxClientKeys = 50_000

// Reasons lists the indeterminate reasons in the order of Counts.Indeterminate.
var Reasons = []string{
	"clock_skew", "receiver_ended", "receiver_gap", "receiver_lagging", "evicted", "rejected", "replayed",
}

const numReasons = 7

// Counts is a set of additive pair counters.
type Counts struct {
	Expected         int64
	DeliveredLive    int64
	DeliveredHistory int64
	Lost             int64
	Late             int64
	Duplicate        int64
	Replayed         int64
	Indeterminate    [numReasons]int64
}

// Delivered is the number of pairs delivered within the window.
func (c *Counts) Delivered() int64 { return c.DeliveredLive + c.DeliveredHistory }

// IndeterminateTotal is the number of pairs excluded from loss.
func (c *Counts) IndeterminateTotal() int64 {
	var n int64
	for _, v := range c.Indeterminate {
		n += v
	}
	return n
}

// LossRatio is lost / (lost + delivered). ok is false when no pair was decided.
func (c *Counts) LossRatio() (ratio float64, ok bool) {
	d := c.Lost + c.Delivered()
	if d == 0 {
		return 0, false
	}
	return float64(c.Lost) / float64(d), true
}

func (c *Counts) add(d *Counts) {
	c.Expected += d.Expected
	c.DeliveredLive += d.DeliveredLive
	c.DeliveredHistory += d.DeliveredHistory
	c.Lost += d.Lost
	c.Late += d.Late
	c.Duplicate += d.Duplicate
	c.Replayed += d.Replayed
	for i := range c.Indeterminate {
		c.Indeterminate[i] += d.Indeterminate[i]
	}
}

// ReasonIndex returns the index of an indeterminate reason, or -1.
func ReasonIndex(reason string) int {
	for i, r := range Reasons {
		if r == reason {
			return i
		}
	}
	return -1
}

// PairKey is a pair class. Every field comes from a fixed allowlist.
type PairKey struct {
	Topic, PubMode, RecvMode, PubLoc, RecvLoc, PubAnon string
}

// ClientKey identifies one publisher-receiver client pair on a topic.
type ClientKey struct {
	PubToken, PubClient, RecvToken, RecvClient, Topic string
}

type clientEntry struct {
	bucket int64
	key    ClientKey
	counts Counts
}

type bucket struct {
	pairs     map[PairKey]*Counts
	clients   map[ClientKey]*list.Element
	truncated bool
}

// Store holds the buckets.
type Store struct {
	retentionMs   int64
	maxClientKeys int
	startMs       int64
	buckets       map[int64]*bucket
	lru           *list.List
}

// New builds an empty store. Data older than retention is dropped by Prune.
func New(retention time.Duration, maxClientKeys int, startMs int64) *Store {
	return &Store{
		retentionMs: retention.Milliseconds(), maxClientKeys: maxClientKeys, startMs: startMs,
		buckets: map[int64]*bucket{}, lru: list.New(),
	}
}

func floorDiv(a, b int64) int64 {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

// Add records d for a pair whose publish time was tP.
func (s *Store) Add(tP int64, pk PairKey, ck ClientKey, d Counts) {
	id := floorDiv(tP, BucketMs)
	b := s.buckets[id]
	if b == nil {
		b = &bucket{pairs: map[PairKey]*Counts{}, clients: map[ClientKey]*list.Element{}}
		s.buckets[id] = b
	}
	pc := b.pairs[pk]
	if pc == nil {
		pc = &Counts{}
		b.pairs[pk] = pc
	}
	pc.add(&d)

	if el, ok := b.clients[ck]; ok {
		el.Value.(*clientEntry).counts.add(&d)
		s.lru.MoveToFront(el)
		return
	}
	e := &clientEntry{bucket: id, key: ck, counts: d}
	b.clients[ck] = s.lru.PushFront(e)
	for s.lru.Len() > s.maxClientKeys {
		s.evictOldestClient()
	}
}

func (s *Store) evictOldestClient() {
	el := s.lru.Back()
	e := s.lru.Remove(el).(*clientEntry)
	if b := s.buckets[e.bucket]; b != nil {
		delete(b.clients, e.key)
		b.truncated = true
	}
}

// Prune drops buckets that ended before now minus the retention.
func (s *Store) Prune(now int64) {
	cutoff := now - s.retentionMs
	for id, b := range s.buckets {
		if (id+1)*BucketMs > cutoff {
			continue
		}
		for _, el := range b.clients {
			s.lru.Remove(el)
		}
		delete(s.buckets, id)
	}
}

// Query selects a range and an optional breakdown.
type Query struct {
	From, To int64
	Topic    string
	By       string
}

// Breakdown values for Query.By.
const (
	ByPair   = "pair"
	ByClient = "client"
)

// PairRow is one by=pair breakdown entry.
type PairRow struct {
	Key    PairKey
	Counts Counts
}

// ClientRow is one by=client breakdown entry.
type ClientRow struct {
	Key    ClientKey
	Counts Counts
}

// Result is the answer to a Query. From and To are the bucket-aligned bounds that were summed.
type Result struct {
	From, To int64
	Partial  bool
	Totals   Counts
	Pairs    []PairRow
	Clients  []ClientRow
}

// Query sums every bucket that overlaps [q.From, q.To).
func (s *Store) Query(now int64, q Query) Result {
	res := Result{
		From: floorDiv(q.From, BucketMs) * BucketMs,
		To:   -floorDiv(-q.To, BucketMs) * BucketMs,
	}
	res.Partial = q.From < s.startMs || q.From < now-s.retentionMs
	pairs := map[PairKey]*Counts{}
	clients := map[ClientKey]*Counts{}
	for id, b := range s.buckets {
		if id*BucketMs < res.From || id*BucketMs >= res.To {
			continue
		}
		for k, c := range b.pairs {
			if q.Topic != "" && k.Topic != q.Topic {
				continue
			}
			res.Totals.add(c)
			if q.By == ByPair {
				agg := pairs[k]
				if agg == nil {
					agg = &Counts{}
					pairs[k] = agg
				}
				agg.add(c)
			}
		}
		if q.By != ByClient {
			continue
		}
		if b.truncated {
			res.Partial = true
		}
		for k, el := range b.clients {
			if q.Topic != "" && k.Topic != q.Topic {
				continue
			}
			agg := clients[k]
			if agg == nil {
				agg = &Counts{}
				clients[k] = agg
			}
			agg.add(&el.Value.(*clientEntry).counts)
		}
	}
	for k, c := range pairs {
		res.Pairs = append(res.Pairs, PairRow{k, *c})
	}
	sort.Slice(res.Pairs, func(i, j int) bool { return pairLess(res.Pairs[i].Key, res.Pairs[j].Key) })
	for k, c := range clients {
		res.Clients = append(res.Clients, ClientRow{k, *c})
	}
	sort.Slice(res.Clients, func(i, j int) bool { return clientLess(res.Clients[i].Key, res.Clients[j].Key) })
	return res
}

func pairLess(a, b PairKey) bool {
	if a.Topic != b.Topic {
		return a.Topic < b.Topic
	}
	if a.PubMode != b.PubMode {
		return a.PubMode < b.PubMode
	}
	if a.RecvMode != b.RecvMode {
		return a.RecvMode < b.RecvMode
	}
	if a.PubLoc != b.PubLoc {
		return a.PubLoc < b.PubLoc
	}
	if a.RecvLoc != b.RecvLoc {
		return a.RecvLoc < b.RecvLoc
	}
	return a.PubAnon < b.PubAnon
}

func clientLess(a, b ClientKey) bool {
	if a.Topic != b.Topic {
		return a.Topic < b.Topic
	}
	if a.PubToken != b.PubToken {
		return a.PubToken < b.PubToken
	}
	if a.PubClient != b.PubClient {
		return a.PubClient < b.PubClient
	}
	if a.RecvToken != b.RecvToken {
		return a.RecvToken < b.RecvToken
	}
	return a.RecvClient < b.RecvClient
}
