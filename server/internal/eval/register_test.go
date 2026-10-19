package eval

import (
	"testing"
	"time"

	"github.com/logos-messaging/logos-delivery-loss-monitor/server/internal/apierr"
)

func wantErr(t *testing.T, err *apierr.Error, status int, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("want %d %s, got success", status, code)
	}
	if err.Status != status || err.Code != code {
		t.Fatalf("want %d %s, got %d %s (%s)", status, code, err.Status, err.Code, err.Detail)
	}
}

func TestRegisterCreatesThenIsIdempotent(t *testing.T) {
	h := newHarness(t)
	o := regOpts{client: 1}
	res, err := h.tryRegister(1, o)
	if err != nil || !res.Created || res.LocationLabel != "eu" {
		t.Fatalf("create: %+v %v", res, err)
	}
	h.advance(5 * time.Second)
	req := o.request(h.now)
	req.NTPSynced = false
	res, err = h.c.Register(h.now, "op", uid("session", 1), req)
	if err != nil || res.Created {
		t.Fatalf("repeat with changed volatile fields: %+v %v", res, err)
	}
}

func TestRegisterStableFieldChangeConflicts(t *testing.T) {
	h := newHarness(t)
	h.register(1, regOpts{client: 1})
	req := regOpts{client: 1}.request(h.now)
	req.RatePerTopic = 0.5
	_, err := h.c.Register(h.now, "op", uid("session", 1), req)
	wantErr(t, err, 409, "session_conflict")
	req = regOpts{client: 1}.request(h.now)
	req.AppVersion = "0.2.0"
	_, err = h.c.Register(h.now, "op", uid("session", 1), req)
	wantErr(t, err, 409, "session_conflict")
}

func TestRegisterTopicOrderIsNotStable(t *testing.T) {
	h := newHarness(t)
	h.register(1, regOpts{client: 1, topics: []string{topicA, topicB}, rate: 0.5})
	req := regOpts{client: 1, topics: []string{topicB, topicA}, rate: 0.5}.request(h.now)
	if _, err := h.c.Register(h.now, "op", uid("session", 1), req); err != nil {
		t.Fatal(err)
	}
}

func TestRegisterBindsSessionAndClientToToken(t *testing.T) {
	h := newHarness(t)
	h.register(1, regOpts{client: 1})
	_, err := h.c.Register(h.now, "intruder", uid("session", 1), regOpts{client: 1}.request(h.now))
	wantErr(t, err, 403, "session_owned")
	_, err = h.tryRegister(2, regOpts{client: 1, token: "intruder"})
	wantErr(t, err, 409, "client_id_conflict")
	if _, err = h.trySend(uid("session", 1), h.hb()); err != nil {
		t.Fatal(err)
	}
	h.tok[uid("session", 1)] = "intruder"
	_, err = h.trySend(uid("session", 1), h.hb())
	wantErr(t, err, 403, "session_owned")
}

func TestClientBindingReleasedAfterLastSessionPruned(t *testing.T) {
	h := newHarness(t)
	h.register(1, regOpts{client: 1})
	h.send(uid("session", 1), h.end())
	_, err := h.tryRegister(2, regOpts{client: 1, token: "other"})
	wantErr(t, err, 409, "client_id_conflict")
	h.advance(h.cfg.Window + h.cfg.Grace + h.cfg.Hold + time.Minute)
	h.register(2, regOpts{client: 1, token: "other"})
}

func TestRegisterRejectsStaleProbeKey(t *testing.T) {
	h := newHarness(t)
	req := regOpts{client: 1}.request(h.now)
	req.ProbeKeyID = "deadbeef"
	_, err := h.c.Register(h.now, "op", uid("session", 1), req)
	wantErr(t, err, 409, "probe_key_changed")
}

func TestRegisterValidation(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(h *harness, r *RegisterRequest)
		status int
		code   string
	}{
		{"network preset", func(h *harness, r *RegisterRequest) { r.Preset = "logos.test" }, 422, "network_mismatch"},
		{"network cluster", func(h *harness, r *RegisterRequest) { r.ClusterID = 9 }, 422, "network_mismatch"},
		{"preset alias", func(h *harness, r *RegisterRequest) { r.Preset = "LogosDev" }, 0, ""},
		{"topic", func(h *harness, r *RegisterRequest) { r.Topics = []string{"/x"} }, 422, "topic_not_allowed"},
		{"rate", func(h *harness, r *RegisterRequest) { r.RatePerTopic = 1.5 }, 422, "rate_too_high"},
		{"rate across topics", func(h *harness, r *RegisterRequest) { r.Topics = []string{topicA, topicB}; r.RatePerTopic = 0.6 }, 422, "rate_too_high"},
		{"payload large", func(h *harness, r *RegisterRequest) { r.PayloadBytes = 1 << 20 }, 422, "payload_too_large"},
		{"payload small", func(h *harness, r *RegisterRequest) { r.PayloadBytes = 10 }, 422, "payload_too_small"},
		{"ephemeral", func(h *harness, r *RegisterRequest) { r.Ephemeral = true }, 422, "ephemeral_mismatch"},
		{"clock", func(h *harness, r *RegisterRequest) { r.ClientTimeMs -= 5 * minuteMs }, 422, "clock_skew"},
		{"clock huge", func(h *harness, r *RegisterRequest) { r.ClientTimeMs = 1 << 62 }, 422, "clock_skew"},
		{"mode", func(h *harness, r *RegisterRequest) { r.Mode = "x" }, 422, "invalid_request"},
		{"client id", func(h *harness, r *RegisterRequest) { r.ClientID = "not-a-uuid" }, 422, "invalid_request"},
		{"no topics", func(h *harness, r *RegisterRequest) { r.Topics = nil }, 422, "invalid_request"},
		{"unknown location maps to other", func(h *harness, r *RegisterRequest) { r.Location = "Mars Base 1" }, 0, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			req := regOpts{client: 1}.request(h.now)
			tc.mutate(h, req)
			res, err := h.c.Register(h.now, "op", uid("session", 1), req)
			if tc.code == "" {
				if err != nil {
					t.Fatalf("unexpected error %v", err)
				}
				if tc.name == "unknown location maps to other" && res.LocationLabel != "other" {
					t.Fatalf("label = %q", res.LocationLabel)
				}
				return
			}
			wantErr(t, err, tc.status, tc.code)
		})
	}
}

func TestRegisterAggregateRateCap(t *testing.T) {
	h := newHarness(t)
	for i := 1; i <= 20; i++ {
		h.register(i, regOpts{client: i, role: "publish", token: "t" + string(rune('a'+i))})
	}
	_, err := h.tryRegister(21, regOpts{client: 21, role: "publish", token: "z"})
	wantErr(t, err, 429, "run_rate_limit")
	if err.RetryAfterMs != 60000 {
		t.Fatalf("retry hint %d", err.RetryAfterMs)
	}
	h.register(22, regOpts{client: 22, role: "receive", token: "z"})
}

func TestRegisterRejectedLeavesNoState(t *testing.T) {
	h := newHarness(t)
	req := regOpts{client: 1}.request(h.now)
	req.Preset = "logos.test"
	_, _ = h.c.Register(h.now, "op", uid("session", 1), req)
	if len(h.c.sessions) != 0 || len(h.c.clientToken) != 0 {
		t.Fatal("a rejected registration left state behind")
	}
}

func TestIngestRejectsBadSentAt(t *testing.T) {
	w := newWorld(t)
	h := w.h
	_, err := h.c.Ingest(h.now, "op", w.a, &Batch{SentAtMs: -1})
	wantErr(t, err, 422, "invalid_time")
	_, err = h.c.Ingest(h.now, "op", w.a, &Batch{SentAtMs: 1 << 60})
	wantErr(t, err, 422, "invalid_time")
}

func TestIngestAckedSeq(t *testing.T) {
	w := newWorld(t)
	h := w.h
	res, err := h.trySend(w.a, h.hb(), h.hb())
	if err != nil || res.AckedSeq != h.seq[w.a] {
		t.Fatalf("acked %d, want %d (%v)", res.AckedSeq, h.seq[w.a], err)
	}
}

func TestEndedSessionIgnoresFurtherRecords(t *testing.T) {
	w := newWorld(t)
	h := w.h
	h.send(w.a, h.end())
	h.send(w.a, h.hb())
	h.expect("lossmon_ingest_records_total", 1, "result", "invalid", "type", "heartbeat")
	req := regOpts{client: 2, role: "receive", loc: "na"}.request(h.now)
	_, err := h.c.Register(h.now, "op", w.a, req)
	wantErr(t, err, 409, "session_conflict")
}
