package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func env(kv ...string) func(string) string {
	m := map[string]string{"LOSSMON_TOKENS_FILE": "t", "LOSSMON_PROBE_KEY_FILE": "k"}
	for i := 0; i+1 < len(kv); i += 2 {
		m["LOSSMON_"+kv[i]] = kv[i+1]
	}
	return func(k string) string { return m[k] }
}

func TestDefaults(t *testing.T) {
	c, err := Load(env())
	if err != nil {
		t.Fatal(err)
	}
	if c.Preset != "logos.dev" || c.ClusterID != 3 || c.Window.Minutes() != 10 || c.SessionIdleTimeout != c.Hold {
		t.Fatalf("unexpected defaults: %+v", c)
	}
	if len(c.Locations) != 6 || c.MaxSessionsPerToken != 20 || c.MaxRateTotal != 20 || c.OutcomeLag != 32*time.Minute {
		t.Fatalf("unexpected defaults: %+v", c)
	}
}

func TestPresetRules(t *testing.T) {
	cases := []struct {
		name string
		kv   []string
		ok   bool
	}{
		{"logos.test", []string{"PRESET", "logos.test"}, true},
		{"alias", []string{"PRESET", "LogosTest"}, true},
		{"cluster mismatch", []string{"PRESET", "logos.dev", "CLUSTER_ID", "2"}, false},
		{"status.prod needs allow", []string{"PRESET", "status.prod", "CLUSTER_ID", "16"}, false},
		{"status.prod allowed", []string{"PRESET", "status.prod", "CLUSTER_ID", "16", "ALLOW_PRESET", "status.prod"}, true},
		{"allowed needs cluster", []string{"PRESET", "status.prod", "ALLOW_PRESET", "status.prod"}, false},
		{"twn never", []string{"PRESET", "twn", "CLUSTER_ID", "1", "ALLOW_PRESET", "twn"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(env(tc.kv...))
			if (err == nil) != tc.ok {
				t.Fatalf("err = %v, want ok=%v", err, tc.ok)
			}
		})
	}
}

func TestInvalidValues(t *testing.T) {
	for _, kv := range [][]string{
		{"WINDOW", "soon"}, {"MAX_SESSIONS", "x"}, {"HEARTBEAT_INTERVAL", "100ms"}, {"TOPICS", " , "},
		{"LOCATIONS", "EU"}, {"MAX_RATE_TOTAL", "-1"}, {"MAX_BATCH_RECORDS", "5000"}, {"MAX_GAPS_PER_SESSION", "1"},
	} {
		if _, err := Load(env(kv...)); err == nil {
			t.Errorf("%v accepted", kv)
		}
	}
	if _, err := Load(func(string) string { return "" }); err == nil {
		t.Error("missing tokens file accepted")
	}
}

func TestProbeKey(t *testing.T) {
	dir := t.TempDir()
	good := strings.Repeat("ab", 32)
	cases := map[string]struct {
		content string
		ok      bool
	}{
		"hex":      {good + "\n", true},
		"0x":       {"0x" + good, true},
		"short":    {strings.Repeat("ab", 31), false},
		"not hex":  {strings.Repeat("zz", 32), false},
		"empty":    {"", false},
		"too long": {strings.Repeat("ab", 33), false},
	}
	for name, tc := range cases {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(tc.content), 0o600); err != nil {
			t.Fatal(err)
		}
		k, err := LoadProbeKey(p)
		if (err == nil) != tc.ok {
			t.Errorf("%s: err = %v", name, err)
		}
		if err == nil && (len(k.ID) != 8 || k.Hex() != "0x"+good) {
			t.Errorf("%s: bad key %+v", name, k)
		}
	}
	if _, err := LoadProbeKey(filepath.Join(dir, "missing")); err == nil {
		t.Error("missing file accepted")
	}
}

func TestCanonicalPreset(t *testing.T) {
	for in, want := range map[string]string{"logosdev": "logos.dev", "Logos.Dev": "logos.dev", "logos_test": "logos.test", "statusprod": "status.prod", "TWN": "twn"} {
		if got := CanonicalPreset(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}

func TestStatsRetention(t *testing.T) {
	c, err := Load(env())
	if err != nil || c.StatsRetention.Hours() != 24 {
		t.Fatalf("default: %v %+v", err, c)
	}
	for _, v := range []string{"9m", "745h", "0s"} {
		if _, err := Load(env("STATS_RETENTION", v)); err == nil {
			t.Errorf("STATS_RETENTION=%s accepted", v)
		}
	}
	for _, v := range []string{"10m", "72h", "744h"} {
		if _, err := Load(env("STATS_RETENTION", v)); err != nil {
			t.Errorf("STATS_RETENTION=%s: %v", v, err)
		}
	}
}

func TestPublicURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"https://lossmon.mywire.org", "https://lossmon.mywire.org"},
		{"https://Lossmon.MyWire.org/", "https://lossmon.mywire.org"},
		{"https://h.example:8443", "https://h.example:8443"},
	}
	for _, tc := range cases {
		c, err := Load(env("PUBLIC_URL", tc.in))
		if err != nil {
			t.Fatalf("%q: %v", tc.in, err)
		}
		if c.PublicURL != tc.want {
			t.Fatalf("%q: PublicURL = %q, want %q", tc.in, c.PublicURL, tc.want)
		}
	}
	for _, in := range []string{
		"http://h", "h", "https://u:p@h", "https://h/x", "https://h?q", "https://h#f", "https://[::1]",
		"https://h_x", "https://h:0", "https://h:70000", "https://h$(id)",
	} {
		if _, err := Load(env("PUBLIC_URL", in)); err == nil || !strings.Contains(err.Error(), "LOSSMON_PUBLIC_URL") {
			t.Fatalf("%q: err = %v, want a LOSSMON_PUBLIC_URL error", in, err)
		}
	}
}

func TestLocationCharset(t *testing.T) {
	for _, loc := range []string{"eu;x", "<b>", "$x", "`id`", "-eu", ".eu", "_eu", "EU"} {
		if _, err := Load(env("LOCATIONS", loc)); err == nil {
			t.Fatalf("location %q accepted", loc)
		}
	}
	c, err := Load(env("LOCATIONS", "eu-west,us.east,ap_1"))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Locations) != 3 {
		t.Fatalf("locations = %v", c.Locations)
	}
}

func TestPresetCharset(t *testing.T) {
	for _, p := range []string{"x;y", "-x", "x y", "x$"} {
		kv := []string{"PRESET", p, "CLUSTER_ID", "16", "ALLOW_PRESET", p}
		if _, err := Load(env(kv...)); err == nil {
			t.Fatalf("preset %q accepted", p)
		}
	}
}
