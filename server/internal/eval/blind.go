package eval

// blindKey identifies one receiver's lost view of one probe.
type blindKey struct {
	probeID string
	uid     uint64
}

type blindEntry struct {
	reason string
	at     int64
	added  int64
	r      *session
}

// blindSet remembers, per (probe_id, receiver), that a receipt was dropped or rejected. A pair found here
// is indeterminate however far its receipt precedes the publisher's outcome event.
type blindSet struct {
	entries map[blindKey]*blindEntry
	fifo    []blindKey
}

func newBlindSet() *blindSet {
	return &blindSet{entries: map[blindKey]*blindEntry{}}
}

func (b *blindSet) reason(probeID string, uid uint64) string {
	if e := b.entries[blindKey{probeID, uid}]; e != nil {
		return e.reason
	}
	return ""
}

// add records the blind spot and returns the entries pushed out to keep the set within max.
func (b *blindSet) add(r *session, probeID, reason string, at, now int64, max int) []*blindEntry {
	k := blindKey{probeID, r.uid}
	if e := b.entries[k]; e != nil {
		if gapRank(reason) > gapRank(e.reason) {
			e.reason = reason
		}
		return nil
	}
	b.entries[k] = &blindEntry{reason: reason, at: at, added: now, r: r}
	b.fifo = append(b.fifo, k)
	var out []*blindEntry
	for len(b.fifo) > max {
		out = append(out, b.pop())
	}
	return out
}

func (b *blindSet) pop() *blindEntry {
	k := b.fifo[0]
	b.fifo = b.fifo[1:]
	e := b.entries[k]
	delete(b.entries, k)
	return e
}

// prune drops entries added more than ttl ago.
func (b *blindSet) prune(now, ttl int64) {
	for len(b.fifo) > 0 && b.entries[b.fifo[0]].added+ttl <= now {
		b.pop()
	}
}
