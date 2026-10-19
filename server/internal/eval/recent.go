package eval

// recentProbes remembers probe ids for a bounded time and a bounded count.
type recentProbes struct {
	at   map[string]int64
	fifo []expiredProbe
}

type expiredProbe struct {
	probeID string
	at      int64
}

func newRecentProbes() *recentProbes {
	return &recentProbes{at: map[string]int64{}}
}

func (r *recentProbes) has(probeID string) bool {
	_, ok := r.at[probeID]
	return ok
}

func (r *recentProbes) add(probeID string, now int64) {
	if r.has(probeID) {
		return
	}
	r.at[probeID] = now
	r.fifo = append(r.fifo, expiredProbe{probeID: probeID, at: now})
}

// prune drops entries older than ttl, and the oldest ones beyond max.
func (r *recentProbes) prune(now, ttl int64, max int) {
	for len(r.fifo) > 0 {
		x := r.fifo[0]
		if x.at+ttl > now && len(r.fifo) <= max {
			return
		}
		r.fifo = r.fifo[1:]
		if r.at[x.probeID] == x.at {
			delete(r.at, x.probeID)
		}
	}
}
