package eval

import (
	"net/http"

	"github.com/logos-messaging/logos-delivery-loss-monitor/server/internal/apierr"
	"github.com/logos-messaging/logos-delivery-loss-monitor/server/internal/metrics"
	"github.com/logos-messaging/logos-delivery-loss-monitor/server/internal/ratelimit"
)

type batchCtx struct {
	results map[string]int
	gaps    map[string]int
	recvSum map[uint64]float64
}

// Ingest applies a batch of records for a session. The batch is applied whole or not at all.
func (c *Core) Ingest(now int64, token, id string, b *Batch) (IngestResult, *apierr.Error) {
	c.m.SetNow(now)
	if b.SentAtMs < 0 || b.SentAtMs >= maxTimeMs {
		return IngestResult{}, badRequest(apierr.InvalidTime, "sent_at_ms is out of range")
	}
	bucket := c.recLimit[token]
	if bucket == nil {
		bucket = ratelimit.NewBucket(c.cfg.TokenRecordsPerSec, 10*c.cfg.TokenRecordsPerSec)
		c.recLimit[token] = bucket
	}
	if ok, wait := bucket.Take(now, float64(len(b.Records))); !ok {
		return IngestResult{}, apierr.Retry(apierr.RecordRateLimited, wait)
	}
	s := c.sessions[id]
	if s == nil {
		return IngestResult{}, apierr.New(http.StatusNotFound, apierr.UnknownSession, "register the session first")
	}
	if s.token != token {
		return IngestResult{}, apierr.New(http.StatusForbidden, apierr.SessionOwned, "session belongs to another token")
	}

	s.addSample(now - b.SentAtMs)
	c.checkSkew(s, now)
	s.lastBatchAt = now

	bc := &batchCtx{results: map[string]int{}, gaps: map[string]int{}, recvSum: map[uint64]float64{}}
	for i := range b.Records {
		r := &b.Records[i]
		res := c.processRecord(s, r, now, bc)
		c.m.IngestRecord(r.Type, res)
		bc.results[res]++
	}
	c.log.Info("batch", "token", token, "session", id, "records", len(b.Records),
		"ok", bc.results[resOK], "duplicate_seq", bc.results[resDuplicateSeq], "invalid", bc.results[resInvalid],
		"over_budget", bc.results[resOverBudget],
		"gap_receiver_gap", bc.gaps[metrics.ReasonReceiverGap], "gap_rejected", bc.gaps[metrics.ReasonRejected])
	c.enforceCaps()
	return IngestResult{AckedSeq: s.lastSeq}, nil
}

func (c *Core) advance(s *session, seq, at int64) {
	s.hasSeq, s.lastSeq, s.lastAt = true, seq, at
}

// processRecord runs the per-record rules in the contract's order and returns the ingest result.
func (c *Core) processRecord(s *session, r *Record, now int64, bc *batchCtx) string {
	if r.Seq < 0 {
		return resInvalid
	}
	if s.hasSeq && r.Seq <= s.lastSeq {
		return resDuplicateSeq
	}
	atOK := r.AtMs >= 0 && r.AtMs < maxTimeMs
	var corr int64
	if atOK {
		corr = s.correct(r.AtMs)
	}
	reachable := atOK && corr <= now+c.maxOff

	if s.hasSeq && r.Seq > s.lastSeq+1 {
		to := now
		if reachable {
			to = corr
		}
		c.addGap(s, s.lastAt, to, metrics.ReasonReceiverGap, bc)
	}

	if s.ended || !c.validRecord(s, r, corr, reachable) {
		if s.ended {
			c.advance(s, r.Seq, s.lastAt)
			return resInvalid
		}
		if reachable {
			c.addGap(s, corr, corr, metrics.ReasonRejected, bc)
			c.advance(s, r.Seq, corr)
		} else {
			c.addGap(s, s.lastAt, now, metrics.ReasonRejected, bc)
			c.advance(s, r.Seq, now)
		}
		return resInvalid
	}

	if corr > s.watermark {
		s.watermark = corr
	}
	if corr < s.registeredAt {
		c.m.Skipped(metrics.SkipBeforeRegistration, 1)
		c.advance(s, r.Seq, corr)
		return resOK
	}
	if r.Type != typeEnd && !c.withinBudget(s, r, corr, bc) {
		c.addGap(s, corr, corr, metrics.ReasonRejected, bc)
		c.advance(s, r.Seq, corr)
		return resOverBudget
	}

	res := c.apply(s, r, corr, now, bc)
	c.advance(s, r.Seq, corr)
	return res
}

func (c *Core) validRecord(s *session, r *Record, corr int64, reachable bool) bool {
	if !reachable {
		return false
	}
	switch r.Type {
	case typeHeartbeat:
		if r.Conn != "connected" && r.Conn != "partially_connected" && r.Conn != "disconnected" {
			return false
		}
		if len(r.Subscribed) > len(s.topics) {
			return false
		}
		for _, t := range r.Subscribed {
			if !s.topicSet[t] {
				return false
			}
		}
		return r.DroppedTotal >= 0 && r.MissedTicksTotal >= 0 && r.ForeignTotal >= 0 &&
			r.UnverifiedProbeTotal >= 0 && r.ReplayedTotal >= 0
	case typePublish:
		if !s.publishes() || !s.topicSet[r.Topic] || !probeIDRe.MatchString(r.ProbeID) || !hashRe.MatchString(r.Hash) {
			return false
		}
		switch r.Outcome {
		case "propagated", "sent", "error", "timeout":
		default:
			return false
		}
		return r.SendAtMs >= 0 && r.SendAtMs < maxTimeMs && r.SendAtMs <= r.AtMs
	case typeReceive:
		if !s.receives() || !s.topicSet[r.Topic] || !probeIDRe.MatchString(r.ProbeID) || !hashRe.MatchString(r.Hash) {
			return false
		}
		if r.Source != "live" && r.Source != "history" {
			return false
		}
		return r.MsgTsMs >= 0 && r.MsgTsMs < maxTimeMs &&
			r.MsgTsMs >= corr-c.cfg.ReceiptMaxAgeMs() && r.MsgTsMs <= corr+c.maxOff
	case typeEnd:
		return true
	}
	return false
}

// withinBudget charges the record against its per-minute bucket.
func (c *Core) withinBudget(s *session, r *Record, corr int64, bc *batchCtx) bool {
	switch r.Type {
	case typePublish:
		limit := ceilInt(1.5*60*s.publishRate()) + 10
		return s.charge(budgetPublish, corr/60000, limit)
	case typeHeartbeat:
		limit := 2*ceilInt(60000/float64(c.cfg.HeartbeatInterval.Milliseconds())) + 2
		return s.charge(budgetHeartbeat, corr/60000, limit)
	case typeReceive:
		sum, ok := bc.recvSum[s.uid]
		if !ok {
			sum = c.publishingRateSharing(s)
			bc.recvSum[s.uid] = sum
		}
		limit := ceilInt(1.5*60*sum) + c.cfg.ReceiptBurst
		return s.charge(budgetReceive, r.MsgTsMs/60000, limit)
	}
	return true
}

func (c *Core) publishingRateSharing(r *session) float64 {
	var sum float64
	for _, p := range c.sessions {
		if !p.live() || !p.publishes() {
			continue
		}
		for _, t := range p.topics {
			if r.topicSet[t] {
				sum += p.rate * float64(len(p.topics))
				break
			}
		}
	}
	return sum
}

func (c *Core) pubLabels(s *session, topic string) metrics.PubLabels {
	return metrics.PubLabels{Topic: topic, Mode: s.mode, Loc: s.loc, Anon: s.anon}
}

func (c *Core) apply(s *session, r *Record, corr, now int64, bc *batchCtx) string {
	switch r.Type {
	case typeHeartbeat:
		c.applyHeartbeat(s, r, corr, bc)
	case typePublish:
		return c.applyPublish(s, r, corr, now, bc)
	case typeReceive:
		c.applyReceive(s, r, corr, now)
	case typeEnd:
		if !s.endSeen {
			s.endSeen, s.ended, s.endedAt = true, true, corr
			s.retireAt = now + c.w + c.g + c.hold
		}
	}
	return resOK
}

func delta(cur int64, last *int64) int {
	d := cur - *last
	if d < 0 {
		d = cur
	}
	*last = cur
	return int(d)
}

func (c *Core) applyHeartbeat(s *session, r *Record, corr int64, bc *batchCtx) {
	s.conn = r.Conn
	for _, t := range r.Subscribed {
		if _, ok := s.subscribedAt[t]; !ok {
			s.subscribedAt[t] = corr
		}
	}
	if s.lastHB != 0 && corr-s.lastHB > 3*c.cfg.HeartbeatInterval.Milliseconds() {
		c.addGap(s, s.lastHB, corr, metrics.ReasonReceiverGap, bc)
	}
	s.lastHB = corr
	c.m.ClientDropped(s.mode, s.loc, delta(r.DroppedTotal, &s.lastDropped))
	c.m.Foreign(s.mode, s.loc, delta(r.ForeignTotal, &s.lastForeign))
	c.m.Unverified(s.mode, s.loc, delta(r.UnverifiedProbeTotal, &s.lastUnverified))
	topic := ""
	if len(s.topics) == 1 {
		topic = s.topics[0]
	}
	c.m.Replayed(topic, delta(r.ReplayedTotal, &s.lastReplayed))
}
