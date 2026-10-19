// Package apierr defines the HTTP error contract shared by the API, auth and eval packages.
package apierr

import "net/http"

// Error codes returned in the {"error": "<code>"} body. The set is fixed so it can back a metric label.
const (
	Unauthorized      = "unauthorized"
	Forbidden         = "forbidden"
	SessionOwned      = "session_owned"
	RateLimited       = "rate_limited"
	RecordRateLimited = "record_rate_limited"
	BodyTooLarge      = "body_too_large"
	BatchTooLarge     = "batch_too_large"
	BadJSON           = "bad_json"
	BadSessionID      = "bad_session_id"
	InvalidTime       = "invalid_time"
	InvalidRequest    = "invalid_request"
	UnknownSession    = "unknown_session"
	SessionConflict   = "session_conflict"
	ClientIDConflict  = "client_id_conflict"
	ProbeKeyChanged   = "probe_key_changed"
	NetworkMismatch   = "network_mismatch"
	TopicNotAllowed   = "topic_not_allowed"
	RateTooHigh       = "rate_too_high"
	PayloadTooLarge   = "payload_too_large"
	PayloadTooSmall   = "payload_too_small"
	EphemeralMismatch = "ephemeral_mismatch"
	ClockSkew         = "clock_skew"
	TokenSessionLimit = "token_session_limit"
	SessionLimit      = "session_limit"
	RunRateLimit      = "run_rate_limit"
	Busy              = "busy"
	Internal          = "internal"
	NotFound          = "not_found"
	MethodNotAllowed  = "method_not_allowed"
)

// Codes lists every code this package defines.
var Codes = []string{
	Unauthorized, Forbidden, SessionOwned, RateLimited, RecordRateLimited, BodyTooLarge, BatchTooLarge,
	BadJSON, BadSessionID, InvalidTime, InvalidRequest, UnknownSession, SessionConflict, ClientIDConflict,
	ProbeKeyChanged, NetworkMismatch, TopicNotAllowed, RateTooHigh, PayloadTooLarge, PayloadTooSmall,
	EphemeralMismatch, ClockSkew, TokenSessionLimit, SessionLimit, RunRateLimit, Busy, Internal, NotFound,
	MethodNotAllowed,
}

// Error is an API failure with its HTTP status. RetryAfterMs is set on 429 and 503 responses.
type Error struct {
	Status       int
	Code         string
	Detail       string
	RetryAfterMs int64
}

func (e *Error) Error() string { return e.Code + ": " + e.Detail }

// New builds an Error without a retry hint.
func New(status int, code, detail string) *Error {
	return &Error{Status: status, Code: code, Detail: detail}
}

// Retry builds a 429 Error that carries a retry hint in milliseconds.
func Retry(code string, retryAfterMs int64) *Error {
	return &Error{Status: http.StatusTooManyRequests, Code: code, RetryAfterMs: retryAfterMs}
}
