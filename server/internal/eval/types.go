// Package eval holds the loss-evaluation state machine. All state is owned by one goroutine (see Engine);
// Core itself takes explicit timestamps so tests can drive it with a fake clock.
package eval

// RegisterRequest is the body of PUT /v1/sessions/{id}.
type RegisterRequest struct {
	ClientID             string   `json:"client_id"`
	Mode                 string   `json:"mode"`
	Role                 string   `json:"role"`
	Location             string   `json:"location"`
	Preset               string   `json:"preset"`
	ClusterID            int      `json:"cluster_id"`
	Topics               []string `json:"topics"`
	RatePerTopic         float64  `json:"rate_per_topic"`
	PayloadBytes         int      `json:"payload_bytes"`
	Ephemeral            bool     `json:"ephemeral"`
	AnonymityLevel       string   `json:"anonymity_level"`
	Reliability          bool     `json:"reliability"`
	AppVersion           string   `json:"app_version"`
	LogosDeliveryVersion string   `json:"logos_delivery_version"`
	ProbeKeyID           string   `json:"probe_key_id"`
	NTPSynced            bool     `json:"ntp_synced"`
	ClientTimeMs         int64    `json:"client_time_ms"`

	// ArrivedMs is when the request reached the server. Zero means the processing time.
	ArrivedMs int64 `json:"-"`
}

// RegisterResult is the body returned by a successful registration.
type RegisterResult struct {
	ServerTimeMs  int64  `json:"server_time_ms"`
	LocationLabel string `json:"location_label"`
	// Created is true for a new session (HTTP 201) and false for an idempotent repeat (HTTP 200).
	Created bool `json:"-"`
}

// Record is one entry of a records batch. Which fields apply depends on Type.
type Record struct {
	Seq  int64  `json:"seq"`
	Type string `json:"type"`
	AtMs int64  `json:"at_ms"`

	Conn                 string   `json:"conn"`
	Subscribed           []string `json:"subscribed"`
	DroppedTotal         int64    `json:"dropped_total"`
	MissedTicksTotal     int64    `json:"missed_ticks_total"`
	ForeignTotal         int64    `json:"foreign_total"`
	UnverifiedProbeTotal int64    `json:"unverified_probe_total"`
	ReplayedTotal        int64    `json:"replayed_total"`

	SendAtMs int64  `json:"send_at_ms"`
	ProbeID  string `json:"probe_id"`
	Hash     string `json:"hash"`
	Topic    string `json:"topic"`
	Outcome  string `json:"outcome"`
	Queued   bool   `json:"queued"`

	Source   string `json:"source"`
	MsgTsMs  int64  `json:"msg_ts_ms"`
	Overflow bool   `json:"overflow"`
}

// Batch is the body of POST /v1/sessions/{id}/records.
type Batch struct {
	SentAtMs int64    `json:"sent_at_ms"`
	Records  []Record `json:"records"`

	// ArrivedMs is when the request reached the server. Zero means the processing time.
	ArrivedMs int64 `json:"-"`
}

func arrivedAt(now, arrived int64) int64 {
	if arrived == 0 {
		return now
	}
	return arrived
}

// IngestResult is the body returned for an applied batch.
type IngestResult struct {
	AckedSeq int64 `json:"acked_seq"`
}

// Record types.
const (
	typeHeartbeat = "heartbeat"
	typePublish   = "publish"
	typeReceive   = "receive"
	typeEnd       = "end"
)

// Per-record ingest results.
const (
	resOK           = "ok"
	resDuplicateSeq = "duplicate_seq"
	resInvalid      = "invalid"
	resOverBudget   = "over_budget"
)

// maxTimeMs bounds every client-supplied millisecond value so arithmetic cannot overflow.
const maxTimeMs = int64(1) << 53
