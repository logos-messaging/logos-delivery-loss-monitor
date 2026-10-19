package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if len(c.Locations) != 6 || c.MaxSessionsPerToken != 20 || c.MaxRateTotal != 20 {
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
