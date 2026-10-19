package api

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"
)

func rfc3339(ms int64) string { return time.UnixMilli(ms).UTC().Format(time.RFC3339) }

func statsPath(kv ...string) string {
	v := url.Values{}
	for i := 0; i+1 < len(kv); i += 2 {
		v.Set(kv[i], kv[i+1])
	}
	return "/v1/stats?" + v.Encode()
}

func TestStatsRequiresQueryScope(t *testing.T) {
	r := newRig(t)
	p := statsPath("from", rfc3339(t0-60_000), "to", rfc3339(t0))
	for _, tc := range []struct {
		name, token string
		want        int
	}{{"no token", "", 401}, {"wrong token", "wrong-token-0123456789-abcdefghijklmn", 401}, {"report token", tokOp, 403}, {"query token", tokQ, 200}} {
		if got := r.status("GET", p, tc.token, nil); got != tc.want {
			t.Errorf("%s: %d, want %d", tc.name, got, tc.want)
		}
	}
	if got := r.status("POST", p, tokQ, `{}`); got != 404 {
		t.Errorf("POST /v1/stats: %d, want 404", got)
	}
}

func TestStatsValidation(t *testing.T) {
	r := newRig(t)
	from, to := rfc3339(t0-60_000), rfc3339(t0)
	for name, p := range map[string]string{
		"missing from":   statsPath("to", to),
		"missing to":     statsPath("from", from),
		"bad from":       statsPath("from", "yesterday", "to", to),
		"to before from": statsPath("from", to, "to", from),
		"equal bounds":   statsPath("from", from, "to", from),
		"unknown by":     statsPath("from", from, "to", to, "by", "token"),
		"unknown topic":  statsPath("from", from, "to", to, "topic", "/other/1/x/raw"),
	} {
		resp, body := r.do("GET", p, tokQ, nil)
		if resp.StatusCode != 422 || !strings.Contains(string(body), `"invalid_request"`) {
			t.Errorf("%s: %d %s, want 422 invalid_request", name, resp.StatusCode, body)
		}
	}
}

func TestStatsShapeOnEmptyStore(t *testing.T) {
	r := newRig(t)
	for _, by := range []string{"", "pair", "client"} {
		resp, body := r.do("GET", statsPath("from", rfc3339(t0-3600_000), "to", rfc3339(t0), "by", by, "topic", topicA), tokQ, nil)
		if resp.StatusCode != 200 {
			t.Fatalf("by=%q: %d %s", by, resp.StatusCode, body)
		}
		var out map[string]any
		if err := json.Unmarshal(body, &out); err != nil {
			t.Fatal(err)
		}
		totals := out["totals"].(map[string]any)
		if out["resolution"] != "10m" || totals["loss_ratio"] != nil || totals["expected"].(float64) != 0 {
			t.Errorf("by=%q: %s", by, body)
		}
		reasons := totals["indeterminate_by_reason"].(map[string]any)
		if len(reasons) != 7 || reasons["evicted"].(float64) != 0 {
			t.Errorf("by=%q reasons: %s", by, body)
		}
		if by != "" {
			if rows, ok := out["breakdown"].([]any); !ok || len(rows) != 0 {
				t.Errorf("by=%q breakdown: %s", by, body)
			}
		} else if _, ok := out["breakdown"]; ok {
			t.Errorf("breakdown without by: %s", body)
		}
	}
}
