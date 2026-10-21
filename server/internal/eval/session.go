package eval

import (
	"sort"
	"strings"

	"github.com/logos-messaging/logos-delivery-loss-monitor/server/internal/metrics"
)

const (
	offsetSamples = 15
	maxReceipts   = 5
)

// gapRank orders coverage-gap reasons by precedence: evicted > rejected > receiver_gap.
func gapRank(reason string) int {
	switch reason {
	case metrics.ReasonEvicted:
		return 3
	case metrics.ReasonRejected:
		return 2
	case metrics.ReasonReceiverGap:
		return 1
	}
	return 0
}

// gap is a period the receiver's reports do not cover. An offline gap is a stretch with no
// heartbeats: only publishes made during it, or up to GRACE before it, are affected, unlike a
// lost-report gap.
type gap struct {
	from, to int64
	reason   string
	offline  bool
}

// stableFields are the registration fields compared for PUT idempotency.
type stableFields struct {
	clientID, mode, role, location, preset string
	clusterID                              int
	topics                                 string
	rate                                   float64
	payloadBytes                           int
	ephemeral                              bool
	anonymity                              string
	reliability                            bool
	appVersion, ldVersion, probeKeyID      string
}

func newStable(req *RegisterRequest, topics []string, locationLabel, preset string) stableFields {
	return stableFields{
		clientID: req.ClientID, mode: req.Mode, role: req.Role, location: locationLabel, preset: preset,
		clusterID: req.ClusterID, topics: strings.Join(topics, "\x00"), rate: req.RatePerTopic,
		payloadBytes: req.PayloadBytes, ephemeral: req.Ephemeral, anonymity: req.AnonymityLevel,
		reliability: req.Reliability, appVersion: req.AppVersion, ldVersion: req.LogosDeliveryVersion,
		probeKeyID: req.ProbeKeyID,
	}
}

// Budget buckets per session, indexed by budgetKind.
const (
	budgetPublish = iota
	budgetReceive
	budgetHeartbeat
	budgetKinds
)

type session struct {
	uid      uint64
	id       string
	token    string
	clientID string
	mode     string
	role     string
	loc      string
	anon     string
	topics   []string
	topicSet map[string]bool
	rate     float64
	stable   stableFields

	registeredAt int64
	lastBatchAt  int64

	samples  []int64
	offset   int64
	skewedAt int64

	hasSeq    bool
	lastSeq   int64
	lastAt    int64
	watermark int64

	subscribedAt                                           map[string]int64
	conn                                                   string
	lastHB                                                 int64
	lastDropped, lastForeign, lastUnverified, lastReplayed int64

	gaps    []gap
	budgets [budgetKinds]map[int64]int
	endSeen bool

	ended    bool
	expired  bool
	endedAt  int64
	retireAt int64
}

func (s *session) publishes() bool { return s.role == "publish" || s.role == "both" }
func (s *session) receives() bool  { return s.role == "receive" || s.role == "both" }
func (s *session) live() bool      { return !s.ended }

// publishRate is the aggregate rate this session declared across its topics.
func (s *session) publishRate() float64 {
	if !s.publishes() {
		return 0
	}
	return s.rate * float64(len(s.topics))
}

func (s *session) addSample(v int64) {
	s.samples = append(s.samples, v)
	if len(s.samples) > offsetSamples {
		s.samples = s.samples[1:]
	}
	sorted := append([]int64(nil), s.samples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	n := len(sorted)
	if n%2 == 1 {
		s.offset = sorted[n/2]
	} else {
		s.offset = sorted[n/2-1]/2 + sorted[n/2]/2
	}
}

// correct maps a client timestamp onto the server clock.
func (s *session) correct(atMs int64) int64 { return atMs + s.offset }

// addGap inserts a coverage gap, keeping the list sorted by start. When the list exceeds max,
// the two oldest gaps merge, which only widens coverage loss.
func (s *session) addGap(from, to int64, reason string, max int) {
	s.insertGap(gap{from: from, to: to, reason: reason}, max)
}

// addOfflineGap is addGap for a stretch in which the receiver was not running.
func (s *session) addOfflineGap(from, to int64, reason string, max int) {
	s.insertGap(gap{from: from, to: to, reason: reason, offline: true}, max)
}

func (s *session) insertGap(g gap, max int) {
	if g.from > g.to {
		g.from, g.to = g.to, g.from
	}
	i := sort.Search(len(s.gaps), func(i int) bool { return s.gaps[i].from > g.from })
	s.gaps = append(s.gaps, gap{})
	copy(s.gaps[i+1:], s.gaps[i:])
	s.gaps[i] = g
	for len(s.gaps) > max {
		a, b := s.gaps[0], s.gaps[1]
		merged := gap{from: a.from, to: a.to, reason: a.reason, offline: a.offline && b.offline}
		if b.to > merged.to {
			merged.to = b.to
		}
		if gapRank(b.reason) > gapRank(merged.reason) {
			merged.reason = b.reason
		}
		s.gaps = append([]gap{merged}, s.gaps[2:]...)
	}
}

// gapReason returns the strongest gap reason overlapping [lo, hi], or "" when covered.
// Offline gaps only count when they overlap [lo, offlineHi].
func (s *session) gapReason(lo, hi, offlineHi int64) string {
	best := ""
	for _, g := range s.gaps {
		end := hi
		if g.offline {
			end = min(hi, offlineHi)
		}
		if g.from <= end && g.to >= lo && gapRank(g.reason) > gapRank(best) {
			best = g.reason
		}
	}
	return best
}

func (s *session) pruneGaps(before int64) {
	kept := s.gaps[:0]
	for _, g := range s.gaps {
		if g.to >= before {
			kept = append(kept, g)
		}
	}
	s.gaps = kept
}

// charge adds one record to the minute bucket and reports whether it fits under limit.
func (s *session) charge(kind int, minute int64, limit int) bool {
	if s.budgets[kind] == nil {
		s.budgets[kind] = map[int64]int{}
	}
	if s.budgets[kind][minute] >= limit {
		return false
	}
	s.budgets[kind][minute]++
	return true
}

func (s *session) pruneBudgets(beforeMinute int64) {
	for k := range s.budgets {
		for m := range s.budgets[k] {
			if m < beforeMinute {
				delete(s.budgets[k], m)
			}
		}
	}
}
