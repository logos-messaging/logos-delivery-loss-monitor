package eval

import (
	"container/heap"
	"container/list"

	"github.com/logos-messaging/logos-delivery-loss-monitor/server/internal/metrics"
)

const (
	statePending = iota
	stateDecided
	stateGone
)

const (
	kindDelivered = iota
	kindLost
	kindIndeterminate
)

// entry ties together everything reported about one probe_id.
type entry struct {
	probeID string
	pub     *publish
	groups  map[uint64]*recvGroup
}

type receipt struct {
	hash   string
	at     int64
	source string
}

// recvGroup holds one receiver session's receipts for a probe_id.
type recvGroup struct {
	e        *entry
	r        *session
	topic    string
	created  int64
	firstAt  int64
	recs     []receipt
	overflow bool
	elem     *list.Element
}

type pairDone struct {
	kind int
	late bool
}

type publish struct {
	probeID, hash, topic string
	sess                 *session
	clientID             string
	tP, sendAt           int64
	queued, skewed       bool
	labels               metrics.PubLabels
	deadline             int64
	state                int
	evaluated            bool
	pairs                []*session
	done                 map[uint64]*pairDone
	decidedAt            int64
	fifo                 *list.Element
	e                    *entry
}

type pubHeap []*publish

func (h pubHeap) Len() int           { return len(h) }
func (h pubHeap) Less(i, j int) bool { return h[i].deadline < h[j].deadline }
func (h pubHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *pubHeap) Push(x any)        { *h = append(*h, x.(*publish)) }
func (h *pubHeap) Pop() any          { o := *h; n := len(o); x := o[n-1]; *h = o[:n-1]; return x }

func (c *Core) pairLabels(p *publish, r *session) metrics.PairLabels {
	return metrics.PairLabels{
		Topic: p.topic, PubMode: p.labels.Mode, RecvMode: r.mode,
		PubLoc: p.labels.Loc, RecvLoc: r.loc, PubAnon: p.labels.Anon,
	}
}

func (c *Core) applyPublish(s *session, r *Record, corr, now int64, bc *batchCtx) string {
	labels := c.pubLabels(s, r.Topic)
	if r.Queued {
		c.m.SendQueued(labels)
	}
	if r.Outcome == "error" || r.Outcome == "timeout" {
		c.m.SendFailed(labels, r.Outcome)
		return resOK
	}
	if e := c.entries[r.ProbeID]; e != nil && e.pub != nil {
		if e.pub.hash != r.Hash {
			c.addGap(s, corr, corr, metrics.ReasonRejected, bc)
			return resInvalid
		}
		return resOK
	}
	if c.expired.has(r.ProbeID) {
		c.m.Skipped(metrics.SkipPublishReportLate, 1)
		return resOK
	}
	e := c.entries[r.ProbeID]
	if e == nil {
		e = &entry{probeID: r.ProbeID, groups: map[uint64]*recvGroup{}}
		c.entries[r.ProbeID] = e
	}
	p := &publish{
		probeID: r.ProbeID, hash: r.Hash, topic: r.Topic, sess: s, clientID: s.clientID,
		tP: corr, sendAt: s.correct(r.SendAtMs), queued: r.Queued, skewed: s.skewedAt != 0,
		labels: labels, deadline: corr + c.w + c.g, e: e, done: map[uint64]*pairDone{},
	}
	e.pub = p
	for _, g := range e.groups {
		if g.elem != nil {
			c.orphanList.Remove(g.elem)
			g.elem = nil
			c.orphanCount--
		}
	}
	p.fifo = c.pendFIFO.PushBack(p)
	heap.Push(&c.heap, p)
	c.m.Published(labels)
	return resOK
}

func (c *Core) applyReceive(s *session, r *Record, corr, now int64) {
	if c.closed.has(r.ProbeID) {
		c.m.Skipped(metrics.SkipAlreadyDecided, 1)
		return
	}
	e := c.entries[r.ProbeID]
	if e == nil {
		e = &entry{probeID: r.ProbeID, groups: map[uint64]*recvGroup{}}
		c.entries[r.ProbeID] = e
	}
	p := e.pub
	if p != nil {
		if p.clientID == s.clientID {
			return
		}
		if p.state == stateDecided {
			c.lateReceipt(p, s, r)
			return
		}
	}
	g := e.groups[s.uid]
	if g == nil {
		g = &recvGroup{e: e, r: s, topic: r.Topic, created: now, firstAt: corr}
		e.groups[s.uid] = g
		if p == nil {
			g.elem = c.orphanList.PushBack(g)
			c.orphanCount++
		}
	}
	switch {
	case r.Overflow:
		g.overflow = true
	case len(g.recs) >= maxReceipts:
		g.overflow = true
	default:
		g.recs = append(g.recs, receipt{hash: r.Hash, at: corr, source: r.Source})
		if corr < g.firstAt {
			g.firstAt = corr
		}
	}
}

// lateReceipt handles a receipt for a publish whose pairs are already decided.
func (c *Core) lateReceipt(p *publish, s *session, r *Record) {
	if r.Overflow {
		return
	}
	if r.Hash != p.hash {
		c.m.Replayed(p.topic, 1)
		return
	}
	d := p.done[s.uid]
	if d == nil {
		return
	}
	l := c.pairLabels(p, s)
	switch {
	case d.kind == kindDelivered, d.kind == kindLost && d.late:
		c.m.Duplicate(l, 1)
	case d.kind == kindLost:
		d.late = true
		c.m.Late(l)
	}
}

// eligible lists the receivers expected to report publish p, ordered by session.
func (c *Core) eligible(p *publish) []*session {
	var out []*session
	for _, r := range c.allSessions() {
		if !r.receives() || r.clientID == p.clientID || !r.topicSet[p.topic] {
			continue
		}
		sub, ok := r.subscribedAt[p.topic]
		if !ok || sub+c.warmup > p.tP {
			continue
		}
		if r.ended && r.endedAt < p.tP {
			continue
		}
		out = append(out, r)
	}
	return out
}

type outcome struct {
	kind     int
	reason   string
	source   string
	deferred bool
	first    int64
	matched  int
	mismatch int
}

// evalPair applies the decision rules, in order, to one (publish, receiver) pair.
func (c *Core) evalPair(p *publish, r *session, now int64) outcome {
	end := p.tP + c.w
	var g *recvGroup
	if p.e != nil {
		g = p.e.groups[r.uid]
	}
	var out outcome
	var best *receipt
	if g != nil {
		for i := range g.recs {
			rc := &g.recs[i]
			if rc.hash != p.hash {
				out.mismatch++
				continue
			}
			out.matched++
			if best == nil || rc.at < best.at {
				best = rc
			}
		}
	}
	if best != nil && best.at <= end {
		out.kind, out.source, out.first = kindDelivered, best.source, best.at
		return out
	}
	out.kind = kindIndeterminate
	switch {
	case p.skewed || (r.skewedAt != 0 && end >= r.skewedAt):
		out.reason = metrics.ReasonClockSkew
	case r.ended && r.endedAt < end:
		out.reason = metrics.ReasonReceiverEnded
	default:
		if reason := c.blindReason(p, r, end); reason != "" {
			out.reason = reason
		} else if g != nil && g.overflow {
			out.reason = metrics.ReasonReplayed
		}
	}
	if out.reason != "" {
		return out
	}
	if r.watermark < end {
		if now >= p.tP+c.w+c.g+c.hold {
			out.reason = metrics.ReasonReceiverLagging
		} else {
			out.deferred = true
		}
		return out
	}
	out.kind = kindLost
	return out
}

// blindReason is the strongest reason the receiver may have missed p's receipts: a gap overlapping the
// window, or a receipt for this probe that the server dropped.
func (c *Core) blindReason(p *publish, r *session, end int64) string {
	reason := r.gapReason(p.tP, end)
	if b := c.blind.reason(p.probeID, r.uid); gapRank(b) > gapRank(reason) {
		return b
	}
	return reason
}

// evaluate decides every pair of p that can be decided now, deferring the rest.
func (c *Core) evaluate(p *publish, now int64) {
	if !p.evaluated {
		p.evaluated = true
		p.pairs = c.eligible(p)
	}
	rest := p.pairs[:0]
	for _, r := range p.pairs {
		out := c.evalPair(p, r, now)
		if out.deferred {
			rest = append(rest, r)
			continue
		}
		c.record(p, r, out)
	}
	p.pairs = rest
	if len(rest) > 0 {
		c.deferred[p] = struct{}{}
		return
	}
	delete(c.deferred, p)
	c.tombstone(p, now)
}

func (c *Core) record(p *publish, r *session, out outcome) {
	l := c.pairLabels(p, r)
	d := &pairDone{kind: out.kind}
	p.done[r.uid] = d
	c.m.Replayed(p.topic, out.mismatch)
	switch out.kind {
	case kindDelivered:
		c.m.Delivered(l, out.source)
		c.m.Duplicate(l, out.matched-1)
		if !p.queued {
			if lat := out.first - p.sendAt; lat >= 0 {
				c.m.Latency(p.labels.Mode, r.mode, out.source, float64(lat)/1000)
			}
		}
	case kindLost:
		c.m.Lost(l)
		if out.matched > 0 {
			d.late = true
			c.m.Late(l)
			c.m.Duplicate(l, out.matched-1)
		}
	default:
		c.m.Indeterminate(l, out.reason)
	}
}

func (c *Core) tombstone(p *publish, now int64) {
	p.state = stateDecided
	p.decidedAt = now
	c.pendFIFO.Remove(p.fifo)
	p.fifo = c.tombFIFO.PushBack(p)
	p.e.groups = nil
}

// dropPublish removes p and its entry without deciding anything further.
func (c *Core) dropPublish(p *publish) {
	if p.fifo != nil {
		if p.state == stateDecided {
			c.tombFIFO.Remove(p.fifo)
		} else {
			c.pendFIFO.Remove(p.fifo)
		}
		p.fifo = nil
	}
	p.state = stateGone
	delete(c.deferred, p)
	if c.entries[p.probeID] == p.e {
		delete(c.entries, p.probeID)
	}
}

// evictPending decides every undecided pair of the oldest pending publish as indeterminate and drops it.
func (c *Core) evictPending() {
	front := c.pendFIFO.Front()
	if front == nil {
		return
	}
	p := front.Value.(*publish)
	if !p.evaluated {
		p.pairs = c.eligible(p)
	}
	for _, r := range p.pairs {
		c.m.Indeterminate(c.pairLabels(p, r), metrics.ReasonEvicted)
	}
	c.dropPublish(p)
}

// evictOrphan drops the oldest orphan group and opens an evicted gap on its receiver.
func (c *Core) evictOrphan(now int64) {
	front := c.orphanList.Front()
	if front == nil {
		return
	}
	g := front.Value.(*recvGroup)
	lo, hi := g.firstAt, g.firstAt
	for _, rc := range g.recs {
		if rc.at > hi {
			hi = rc.at
		}
	}
	c.removeOrphan(g)
	c.blindProbe(g.r, g.e.probeID, metrics.ReasonEvicted, hi, now)
	c.addGap(g.r, lo, hi, metrics.ReasonEvicted, nil)
	c.m.Skipped(metrics.SkipEvicted, 1)
}

func (c *Core) removeOrphan(g *recvGroup) {
	c.orphanList.Remove(g.elem)
	g.elem = nil
	c.orphanCount--
	delete(g.e.groups, g.r.uid)
	if len(g.e.groups) == 0 && g.e.pub == nil {
		delete(c.entries, g.e.probeID)
	}
}

func (c *Core) enforceCaps(now int64) {
	for c.pendFIFO.Len() > c.cfg.MaxPendingPublishes {
		c.evictPending()
	}
	for c.tombFIFO.Len() > c.cfg.MaxPendingPublishes {
		c.dropPublish(c.tombFIFO.Front().Value.(*publish))
	}
	for c.orphanCount > c.cfg.MaxOrphans {
		c.evictOrphan(now)
	}
}
