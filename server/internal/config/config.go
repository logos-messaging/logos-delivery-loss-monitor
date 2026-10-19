// Package config loads and validates the server configuration from LOSSMON_* environment variables.
package config

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the validated server configuration. Durations are exposed as time.Duration;
// the *Ms helpers give the millisecond values the evaluation engine works in.
type Config struct {
	Listen        string
	MetricsListen string
	TokensFile    string
	ProbeKeyFile  string

	Preset      string
	ClusterID   int
	AllowPreset string

	Topics    []string
	Locations []string
	Ephemeral bool

	MaxRatePerClient  float64
	MaxPayloadBytes   int
	HeartbeatInterval time.Duration
	MaxBatchRecords   int
	MaxBodyBytes      int64

	Window             time.Duration
	Grace              time.Duration
	Hold               time.Duration
	ReceiverWarmup     time.Duration
	MaxClockOffset     time.Duration
	OutcomeLag         time.Duration
	LossRatioWindow    time.Duration
	SessionIdleTimeout time.Duration
	StatsRetention     time.Duration

	MaxSessions         int
	MaxSessionsPerToken int
	MaxRateTotal        float64
	TokenReqPerSec      float64
	TokenRecordsPerSec  float64
	ReceiptBurst        int
	MaxGapsPerSession   int
	MaxPendingPublishes int
	MaxOrphans          int

	LogLevel string
}

// Known presets and their cluster ids.
const (
	PresetLogosDev  = "logos.dev"
	PresetLogosTest = "logos.test"
	presetTWN       = "twn"
)

var knownClusters = map[string]int{PresetLogosDev: 3, PresetLogosTest: 2}

// CanonicalPreset normalises a preset name the way the logos-delivery node does.
func CanonicalPreset(s string) string {
	l := strings.ToLower(strings.TrimSpace(s))
	switch strings.NewReplacer(".", "", "-", "", "_", "").Replace(l) {
	case "logosdev":
		return PresetLogosDev
	case "logostest":
		return PresetLogosTest
	case "statusprod":
		return "status.prod"
	}
	return l
}

// WindowMs returns W in milliseconds.
func (c *Config) WindowMs() int64 { return c.Window.Milliseconds() }

// GraceMs returns G in milliseconds.
func (c *Config) GraceMs() int64 { return c.Grace.Milliseconds() }

// HoldMs returns HOLD in milliseconds.
func (c *Config) HoldMs() int64 { return c.Hold.Milliseconds() }

// ReceiptMaxAgeMs returns the oldest receipt, by message timestamp, the server accepts: W+G+HOLD.
func (c *Config) ReceiptMaxAgeMs() int64 { return (c.Window + c.Grace + c.Hold).Milliseconds() }

// MaxClockOffsetMs returns the clock offset bound in milliseconds.
func (c *Config) MaxClockOffsetMs() int64 { return c.MaxClockOffset.Milliseconds() }

// OutcomeLagMs returns the longest a publisher's outcome event may trail the receipts it explains.
func (c *Config) OutcomeLagMs() int64 { return c.OutcomeLag.Milliseconds() }

// Load reads the configuration through getenv and validates it.
func Load(getenv func(string) string) (*Config, error) {
	l := loader{get: getenv}
	c := &Config{
		Listen:        l.str("LISTEN", ":8080"),
		MetricsListen: l.str("METRICS_LISTEN", ":9102"),
		TokensFile:    l.str("TOKENS_FILE", ""),
		ProbeKeyFile:  l.str("PROBE_KEY_FILE", ""),
		AllowPreset:   CanonicalPreset(l.str("ALLOW_PRESET", "")),
		Topics:        l.list("TOPICS", "/lossmon/1/probe-0/raw"),
		Locations:     l.list("LOCATIONS", "af,as,eu,na,oc,sa"),
		Ephemeral:     l.boolean("EPHEMERAL", false),
		LogLevel:      l.str("LOG_LEVEL", "info"),

		MaxRatePerClient: l.float("MAX_RATE_PER_CLIENT", 1.0),
		MaxPayloadBytes:  l.integer("MAX_PAYLOAD_BYTES", 4096),
		MaxBatchRecords:  l.integer("MAX_BATCH_RECORDS", 2000),
		MaxBodyBytes:     1 << 20,

		HeartbeatInterval: l.duration("HEARTBEAT_INTERVAL", 10*time.Second),
		Window:            l.duration("WINDOW", 10*time.Minute),
		Grace:             l.duration("GRACE", 30*time.Second),
		Hold:              l.duration("HOLD", 10*time.Minute),
		ReceiverWarmup:    l.duration("RECEIVER_WARMUP", 30*time.Second),
		MaxClockOffset:    l.duration("MAX_CLOCK_OFFSET", 60*time.Second),
		OutcomeLag:        l.duration("OUTCOME_LAG", 32*time.Minute),
		LossRatioWindow:   l.duration("LOSS_RATIO_WINDOW", time.Hour),
		StatsRetention:    l.duration("STATS_RETENTION", 24*time.Hour),

		MaxSessions:         l.integer("MAX_SESSIONS", 200),
		MaxSessionsPerToken: l.integer("MAX_SESSIONS_PER_TOKEN", 20),
		MaxRateTotal:        l.float("MAX_RATE_TOTAL", 20),
		TokenReqPerSec:      l.float("TOKEN_REQ_PER_SEC", 20),
		TokenRecordsPerSec:  l.float("TOKEN_RECORDS_PER_SEC", 2000),
		ReceiptBurst:        l.integer("RECEIPT_BURST", 1000),
		MaxGapsPerSession:   l.integer("MAX_GAPS_PER_SESSION", 1024),
		MaxPendingPublishes: l.integer("MAX_PENDING_PUBLISHES", 200000),
		MaxOrphans:          l.integer("MAX_ORPHANS", 200000),
	}
	c.SessionIdleTimeout = l.duration("SESSION_IDLE_TIMEOUT", c.Hold)
	preset := l.str("PRESET", PresetLogosDev)
	clusterRaw := l.str("CLUSTER_ID", "")
	if l.err != nil {
		return nil, l.err
	}

	c.Preset = CanonicalPreset(preset)
	known, isKnown := knownClusters[c.Preset]
	switch {
	case c.Preset == presetTWN:
		return nil, errors.New("preset twn is not supported: RLN presets are out of scope")
	case !isKnown && c.Preset != c.AllowPreset:
		return nil, fmt.Errorf("preset %q needs LOSSMON_ALLOW_PRESET=%s", c.Preset, c.Preset)
	}
	if clusterRaw != "" {
		id, err := strconv.Atoi(clusterRaw)
		if err != nil {
			return nil, fmt.Errorf("LOSSMON_CLUSTER_ID: %w", err)
		}
		c.ClusterID = id
	}
	switch {
	case isKnown && c.ClusterID == 0:
		c.ClusterID = known
	case isKnown && c.ClusterID != known:
		return nil, fmt.Errorf("preset %s uses cluster %d, got LOSSMON_CLUSTER_ID=%d", c.Preset, known, c.ClusterID)
	case !isKnown && c.ClusterID == 0:
		return nil, fmt.Errorf("preset %s needs LOSSMON_CLUSTER_ID", c.Preset)
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Config) validate() error {
	if c.TokensFile == "" {
		return errors.New("LOSSMON_TOKENS_FILE is required")
	}
	if c.ProbeKeyFile == "" {
		return errors.New("LOSSMON_PROBE_KEY_FILE is required")
	}
	if len(c.Topics) == 0 {
		return errors.New("LOSSMON_TOPICS must list at least one content topic")
	}
	seen := map[string]bool{}
	for _, t := range c.Topics {
		if len(t) > 128 || strings.ContainsAny(t, " \t\r\n") {
			return fmt.Errorf("invalid content topic %q", t)
		}
		if seen[t] {
			return fmt.Errorf("duplicate content topic %q", t)
		}
		seen[t] = true
	}
	for _, loc := range c.Locations {
		if loc == "" || len(loc) > 32 || loc != strings.ToLower(loc) || strings.ContainsAny(loc, " \t\r\n") {
			return fmt.Errorf("invalid location %q: use short lowercase labels", loc)
		}
	}
	if c.HeartbeatInterval < time.Second || c.HeartbeatInterval > time.Minute {
		return errors.New("LOSSMON_HEARTBEAT_INTERVAL must be within 1s..60s")
	}
	if c.MaxBatchRecords < 1 || c.MaxBatchRecords > 2000 {
		return errors.New("LOSSMON_MAX_BATCH_RECORDS must be within 1..2000")
	}
	if c.MaxPayloadBytes < 64 || c.MaxPayloadBytes > 65536 {
		return errors.New("LOSSMON_MAX_PAYLOAD_BYTES must be within 64..65536")
	}
	for name, d := range map[string]time.Duration{
		"WINDOW": c.Window, "GRACE": c.Grace, "HOLD": c.Hold, "RECEIVER_WARMUP": c.ReceiverWarmup,
		"MAX_CLOCK_OFFSET": c.MaxClockOffset, "OUTCOME_LAG": c.OutcomeLag, "LOSS_RATIO_WINDOW": c.LossRatioWindow,
		"SESSION_IDLE_TIMEOUT": c.SessionIdleTimeout,
	} {
		if d < time.Second {
			return fmt.Errorf("LOSSMON_%s must be at least 1s", name)
		}
	}
	if c.StatsRetention < 10*time.Minute || c.StatsRetention > 31*24*time.Hour {
		return errors.New("LOSSMON_STATS_RETENTION must be within 10m..744h")
	}
	for name, v := range map[string]float64{
		"MAX_RATE_PER_CLIENT": c.MaxRatePerClient, "MAX_RATE_TOTAL": c.MaxRateTotal,
		"TOKEN_REQ_PER_SEC": c.TokenReqPerSec, "TOKEN_RECORDS_PER_SEC": c.TokenRecordsPerSec,
	} {
		if v <= 0 {
			return fmt.Errorf("LOSSMON_%s must be positive", name)
		}
	}
	for name, v := range map[string]int{
		"MAX_SESSIONS": c.MaxSessions, "MAX_SESSIONS_PER_TOKEN": c.MaxSessionsPerToken,
		"RECEIPT_BURST": c.ReceiptBurst, "MAX_GAPS_PER_SESSION": c.MaxGapsPerSession,
		"MAX_PENDING_PUBLISHES": c.MaxPendingPublishes, "MAX_ORPHANS": c.MaxOrphans,
	} {
		if v < 1 {
			return fmt.Errorf("LOSSMON_%s must be at least 1", name)
		}
	}
	if c.MaxGapsPerSession < 2 {
		return errors.New("LOSSMON_MAX_GAPS_PER_SESSION must be at least 2")
	}
	return nil
}

// ProbeKey is the run-wide probe authentication key served to clients.
type ProbeKey struct {
	Bytes [32]byte
	// ID is the first 8 hex characters of SHA-256(key).
	ID string
}

// Hex returns the key as 0x-prefixed lowercase hex.
func (k ProbeKey) Hex() string { return "0x" + hex.EncodeToString(k.Bytes[:]) }

// LoadProbeKey reads a 32-byte hex key from path.
func LoadProbeKey(path string) (ProbeKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ProbeKey{}, fmt.Errorf("probe key: %w", err)
	}
	s := strings.TrimPrefix(strings.TrimSpace(string(raw)), "0x")
	b, err := hex.DecodeString(s)
	if err != nil {
		return ProbeKey{}, errors.New("probe key: file is not hex")
	}
	if len(b) != 32 {
		return ProbeKey{}, fmt.Errorf("probe key: need 32 bytes, got %d", len(b))
	}
	var k ProbeKey
	copy(k.Bytes[:], b)
	sum := sha256.Sum256(b)
	k.ID = hex.EncodeToString(sum[:])[:8]
	return k, nil
}

type loader struct {
	get func(string) string
	err error
}

func (l *loader) raw(name string) string { return strings.TrimSpace(l.get("LOSSMON_" + name)) }

func (l *loader) fail(name string, err error) {
	if l.err == nil {
		l.err = fmt.Errorf("LOSSMON_%s: %w", name, err)
	}
}

func (l *loader) str(name, def string) string {
	if v := l.raw(name); v != "" {
		return v
	}
	return def
}

func (l *loader) list(name, def string) []string {
	var out []string
	for _, p := range strings.Split(l.str(name, def), ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (l *loader) boolean(name string, def bool) bool {
	v := l.raw(name)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		l.fail(name, err)
	}
	return b
}

func (l *loader) integer(name string, def int) int {
	v := l.raw(name)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		l.fail(name, err)
	}
	return n
}

func (l *loader) float(name string, def float64) float64 {
	v := l.raw(name)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		l.fail(name, err)
	}
	return f
}

func (l *loader) duration(name string, def time.Duration) time.Duration {
	v := l.raw(name)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		l.fail(name, err)
	}
	return d
}
