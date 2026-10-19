package eval

import (
	"container/heap"

	"github.com/logos-messaging/logos-delivery-loss-monitor/server/internal/metrics"
)

// Tick advances time: it decides due publishes, retries deferred pairs, expires orphans,
// tombstones and idle sessions, and refreshes the gauges.
func (c *Core) Tick(now int64) {
	c.m.SetNow(now)
	for p := range c.deferred {
		c.evaluate(p, now)
	}
	for len(c.heap) > 0 && c.heap[0].deadline <= now {
		p := heap.Pop(&c.heap).(*publish)
		if p.state == statePending {
			c.evaluate(p, now)
		}
	}
	c.expireTombstones(now)
	c.expireOrphans(now)
	c.expireSessions(now)
	c.refreshGauges(now)
}

func (c *Core) expireTombstones(now int64) {
	for f := c.tombFIFO.Front(); f != nil; f = c.tombFIFO.Front() {
		p := f.Value.(*publish)
		if p.decidedAt+c.w > now {
			return
		}
		c.dropPublish(p)
		c.closed.add(p.probeID, now)
	}
}

func (c *Core) expireOrphans(now int64) {
	ttl := c.w + c.g + c.hold
	for f := c.orphanList.Front(); f != nil; f = c.orphanList.Front() {
		g := f.Value.(*recvGroup)
		if g.created+ttl > now {
			break
		}
		n := len(g.recs)
		if n == 0 {
			n = 1
		}
		if g.firstAt < c.startMs+c.w+c.g {
			c.m.Skipped(metrics.SkipBeforeStart, n)
		} else {
			c.m.Unmatched(g.topic, n)
		}
		c.expired.add(g.e.probeID, now)
		c.removeOrphan(g)
	}
	c.expired.prune(now, ttl, c.cfg.MaxOrphans)
	c.closed.prune(now, ttl, c.cfg.MaxOrphans)
	c.blind.prune(now, c.w+ttl)
}

func (c *Core) expireSessions(now int64) {
	retain := c.w + c.g + c.hold
	for id, s := range c.sessions {
		if !s.ended && now-s.lastBatchAt >= c.cfg.SessionIdleTimeout.Milliseconds() {
			s.ended, s.expired = true, true
			s.endedAt = s.watermark
			if s.endedAt == 0 {
				s.endedAt = s.registeredAt
			}
			s.retireAt = now + retain
			delete(c.sessions, id)
			c.retired[s.uid] = s
			continue
		}
		if s.ended && s.retireAt <= now {
			delete(c.sessions, id)
			c.releaseClient(s)
		}
	}
	for uid, s := range c.retired {
		if s.retireAt <= now {
			delete(c.retired, uid)
			c.releaseClient(s)
		}
	}
	gapBefore := now - (retain + c.w + c.pad)
	budgetBefore := (now-retain)/60000 - 2
	for _, s := range c.allSessions() {
		s.pruneGaps(gapBefore)
		s.pruneBudgets(budgetBefore)
	}
}

func (c *Core) refreshGauges(now int64) {
	skewed := 0
	counts := map[metrics.SessionKey]int{}
	for _, s := range c.sessions {
		if !s.live() {
			continue
		}
		if s.skewedAt != 0 {
			skewed++
		}
		counts[metrics.SessionKey{Mode: s.mode, Loc: s.loc, Conn: s.conn}]++
	}
	c.m.SetActiveSessions(counts)
	c.m.SetGauges(skewed, c.pendFIFO.Len(), c.orphanCount)
	c.m.Tick(now)
}
