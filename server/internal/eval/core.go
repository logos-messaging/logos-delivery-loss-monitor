package eval

import (
	"container/list"
	"log/slog"
	"math"
	"net/http"
	"regexp"
	"sort"

	"github.com/logos-messaging/logos-delivery-loss-monitor/server/internal/apierr"
	"github.com/logos-messaging/logos-delivery-loss-monitor/server/internal/config"
	"github.com/logos-messaging/logos-delivery-loss-monitor/server/internal/metrics"
	"github.com/logos-messaging/logos-delivery-loss-monitor/server/internal/ratelimit"
	"github.com/logos-messaging/logos-delivery-loss-monitor/server/internal/stats"
)

var (
	uuidRe    = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	probeIDRe = regexp.MustCompile(`^0x[0-9a-f]{32}$`)
	hashRe    = regexp.MustCompile(`^0x[0-9a-f]{64}$`)
)

// IsUUID reports whether s is a lowercase hyphenated UUID.
func IsUUID(s string) bool { return uuidRe.MatchString(s) }

const registerRetryAfterMs = 60_000

// Core is the evaluation state. It is not safe for concurrent use: exactly one goroutine may call it.
type Core struct {
	cfg        *config.Config
	m          *metrics.Metrics
	stats      *stats.Store
	log        *slog.Logger
	probeKeyID string
	startMs    int64

	w, g, hold, warmup, maxOff, pad int64

	sessions    map[string]*session
	retired     map[uint64]*session
	nextUID     uint64
	clientRefs  map[string]int
	clientToken map[string]string
	recLimit    map[string]*ratelimit.Bucket

	entries     map[string]*entry
	heap        pubHeap
	pendFIFO    *list.List
	tombFIFO    *list.List
	orphanList  *list.List
	orphanCount int
	deferred    map[*publish]struct{}
	expired     *recentProbes
	closed      *recentProbes
	blind       *blindSet
}

// NewCore builds an empty state. startMs is the server start time on the evaluation clock.
func NewCore(cfg *config.Config, m *metrics.Metrics, log *slog.Logger, probeKeyID string, startMs int64) *Core {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	c := &Core{
		cfg: cfg, m: m, log: log, probeKeyID: probeKeyID, startMs: startMs,
		stats: stats.New(cfg.StatsRetention, stats.DefaultMaxClientKeys, startMs),
		w:     cfg.WindowMs(), g: cfg.GraceMs(), hold: cfg.HoldMs(),
		warmup: cfg.ReceiverWarmup.Milliseconds(), maxOff: cfg.MaxClockOffsetMs(),
		sessions: map[string]*session{}, retired: map[uint64]*session{},
		clientRefs: map[string]int{}, clientToken: map[string]string{}, recLimit: map[string]*ratelimit.Bucket{},
		entries: map[string]*entry{}, pendFIFO: list.New(), tombFIFO: list.New(), orphanList: list.New(),
		deferred: map[*publish]struct{}{}, expired: newRecentProbes(), closed: newRecentProbes(), blind: newBlindSet(),
	}
	c.pad = c.g
	return c
}

// Stats answers a stats query from the aggregates.
func (c *Core) Stats(now int64, q stats.Query) stats.Result {
	return c.stats.Query(now, q)
}

func (c *Core) addGap(s *session, from, to int64, reason string, bc *batchCtx) {
	s.addGap(from, to+c.pad, reason, c.cfg.MaxGapsPerSession)
	c.m.CoverageGap(reason)
	if bc != nil {
		bc.gaps[reason]++
	}
}

// addOfflineGap records a heartbeat gap: the receiver was not running between from and to.
func (c *Core) addOfflineGap(s *session, from, to int64, reason string, bc *batchCtx) {
	s.addOfflineGap(from, to+c.pad, reason, c.cfg.MaxGapsPerSession)
	c.m.CoverageGap(reason)
	if bc != nil {
		bc.gaps[reason]++
	}
}

// addLooseGap is addGap for drops whose probe is unknown. It also covers publishes reported up to W later.
func (c *Core) addLooseGap(s *session, from, to int64, reason string, bc *batchCtx) {
	s.addGap(from, to+c.pad+c.w, reason, c.cfg.MaxGapsPerSession)
	c.m.CoverageGap(reason)
	if bc != nil {
		bc.gaps[reason]++
	}
}

// blindProbe marks the receiver's view of probeID as lost. Entries pushed out of the bounded set
// fall back to a loose time gap.
func (c *Core) blindProbe(s *session, probeID, reason string, at, now int64) {
	for _, e := range c.blind.add(s, probeID, reason, at, now, c.cfg.MaxOrphans) {
		e.r.addGap(e.at, e.at+c.pad+c.w, e.reason, c.cfg.MaxGapsPerSession)
	}
}

func (c *Core) liveCounts(token string) (perToken, total int, rate float64) {
	for _, s := range c.sessions {
		if !s.live() {
			continue
		}
		total++
		rate += s.publishRate()
		if s.token == token {
			perToken++
		}
	}
	return
}

func badRequest(code, detail string) *apierr.Error {
	return apierr.New(http.StatusUnprocessableEntity, code, detail)
}

// Register creates a session, or confirms an identical one, for the token.
func (c *Core) Register(now int64, token, id string, req *RegisterRequest) (RegisterResult, *apierr.Error) {
	c.m.SetNow(now)
	ex := c.sessions[id]
	if ex != nil && ex.token != token {
		return RegisterResult{}, apierr.New(http.StatusForbidden, apierr.SessionOwned, "session belongs to another token")
	}
	if bound, ok := c.clientToken[req.ClientID]; ok && bound != token {
		return RegisterResult{}, apierr.New(http.StatusConflict, apierr.ClientIDConflict, "client_id is bound to another token")
	}
	if req.ProbeKeyID != c.probeKeyID {
		return RegisterResult{}, apierr.New(http.StatusConflict, apierr.ProbeKeyChanged, "fetch /v1/config again")
	}
	arrived := arrivedAt(now, req.ArrivedMs)
	topics, apiErr := c.validateRegister(arrived, req)
	if apiErr != nil {
		return RegisterResult{}, apiErr
	}
	label := c.m.Location(req.Location)
	stable := newStable(req, topics, label, config.CanonicalPreset(req.Preset))

	if ex != nil {
		if ex.ended || ex.stable != stable {
			return RegisterResult{}, apierr.New(http.StatusConflict, apierr.SessionConflict, "session exists with different parameters")
		}
		ex.addSample(arrived - req.ClientTimeMs)
		c.checkSkew(ex, now)
		return RegisterResult{ServerTimeMs: now, LocationLabel: label}, nil
	}

	perToken, total, rate := c.liveCounts(token)
	newRate := req.RatePerTopic * float64(len(topics))
	if req.Role == "receive" {
		newRate = 0
	}
	switch {
	case perToken >= c.cfg.MaxSessionsPerToken:
		return RegisterResult{}, apierr.Retry(apierr.TokenSessionLimit, registerRetryAfterMs)
	case total >= c.cfg.MaxSessions:
		return RegisterResult{}, apierr.Retry(apierr.SessionLimit, registerRetryAfterMs)
	case rate+newRate > c.cfg.MaxRateTotal+1e-9:
		return RegisterResult{}, apierr.Retry(apierr.RunRateLimit, registerRetryAfterMs)
	}

	c.nextUID++
	s := &session{
		uid: c.nextUID, id: id, token: token, clientID: req.ClientID, mode: req.Mode, role: req.Role,
		loc: label, anon: req.AnonymityLevel, topics: topics, topicSet: map[string]bool{}, rate: req.RatePerTopic,
		stable: stable, registeredAt: now, lastBatchAt: now, lastAt: now,
		subscribedAt: map[string]int64{}, conn: "unknown",
	}
	if req.Role == "receive" {
		s.rate = 0
	}
	for _, t := range topics {
		s.topicSet[t] = true
	}
	s.addSample(arrived - req.ClientTimeMs)
	c.sessions[id] = s
	c.clientRefs[req.ClientID]++
	c.clientToken[req.ClientID] = token
	return RegisterResult{ServerTimeMs: now, LocationLabel: label, Created: true}, nil
}

func (c *Core) validateRegister(now int64, req *RegisterRequest) ([]string, *apierr.Error) {
	if !uuidRe.MatchString(req.ClientID) {
		return nil, badRequest(apierr.InvalidRequest, "client_id must be a lowercase UUID")
	}
	if req.Mode != "core" && req.Mode != "edge" {
		return nil, badRequest(apierr.InvalidRequest, "mode must be core or edge")
	}
	if req.Role != "publish" && req.Role != "receive" && req.Role != "both" {
		return nil, badRequest(apierr.InvalidRequest, "role must be publish, receive or both")
	}
	if req.AnonymityLevel != "none" && req.AnonymityLevel != "preferred" && req.AnonymityLevel != "required" {
		return nil, badRequest(apierr.InvalidRequest, "anonymity_level must be none, preferred or required")
	}
	if len(req.AppVersion) > 64 || len(req.LogosDeliveryVersion) > 64 || len(req.ProbeKeyID) > 16 {
		return nil, badRequest(apierr.InvalidRequest, "version fields are too long")
	}
	if len(req.Topics) == 0 || len(req.Topics) > len(c.cfg.Topics) {
		return nil, badRequest(apierr.InvalidRequest, "topics must be a non-empty subset of the allowlist")
	}
	if math.IsNaN(req.RatePerTopic) || math.IsInf(req.RatePerTopic, 0) || req.RatePerTopic < 0 {
		return nil, badRequest(apierr.InvalidRequest, "rate_per_topic must be a non-negative number")
	}
	if config.CanonicalPreset(req.Preset) != c.cfg.Preset || req.ClusterID != c.cfg.ClusterID {
		return nil, badRequest(apierr.NetworkMismatch, "preset or cluster differs from the server's")
	}
	allowed := map[string]bool{}
	for _, t := range c.cfg.Topics {
		allowed[t] = true
	}
	seen := map[string]bool{}
	topics := make([]string, 0, len(req.Topics))
	for _, t := range req.Topics {
		if !allowed[t] {
			return nil, badRequest(apierr.TopicNotAllowed, "content topic is not in the allowlist")
		}
		if seen[t] {
			return nil, badRequest(apierr.InvalidRequest, "duplicate topic")
		}
		seen[t] = true
		topics = append(topics, t)
	}
	sort.Strings(topics)
	if req.Role != "receive" {
		if req.RatePerTopic <= 0 {
			return nil, badRequest(apierr.InvalidRequest, "rate_per_topic must be positive for publishers")
		}
		if req.RatePerTopic*float64(len(topics)) > c.cfg.MaxRatePerClient+1e-9 {
			return nil, badRequest(apierr.RateTooHigh, "rate_per_topic x topics exceeds the per-client limit")
		}
	}
	if req.PayloadBytes < 64 {
		return nil, badRequest(apierr.PayloadTooSmall, "payload_bytes is below 64")
	}
	if req.PayloadBytes > c.cfg.MaxPayloadBytes {
		return nil, badRequest(apierr.PayloadTooLarge, "payload_bytes exceeds the server limit")
	}
	if req.Ephemeral != c.cfg.Ephemeral {
		return nil, badRequest(apierr.EphemeralMismatch, "ephemeral differs from the server's setting")
	}
	if req.ClientTimeMs < 0 || req.ClientTimeMs >= maxTimeMs || abs(now-req.ClientTimeMs) > c.maxOff {
		return nil, badRequest(apierr.ClockSkew, "client clock differs from the server's by too much")
	}
	return topics, nil
}

func (c *Core) checkSkew(s *session, now int64) {
	if s.skewedAt == 0 && abs(s.offset) > c.maxOff {
		s.skewedAt = now
	}
}

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func (c *Core) releaseClient(s *session) {
	c.clientRefs[s.clientID]--
	if c.clientRefs[s.clientID] <= 0 {
		delete(c.clientRefs, s.clientID)
		delete(c.clientToken, s.clientID)
	}
}

func (c *Core) allSessions() []*session {
	out := make([]*session, 0, len(c.sessions)+len(c.retired))
	for _, s := range c.sessions {
		out = append(out, s)
	}
	for _, s := range c.retired {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].uid < out[j].uid })
	return out
}

func ceilInt(f float64) int { return int(math.Ceil(f - 1e-9)) }
