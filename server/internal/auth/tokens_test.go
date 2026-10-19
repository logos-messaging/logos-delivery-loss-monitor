package auth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	tokA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	tokB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func write(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "tokens")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLookupAndReload(t *testing.T) {
	p := write(t, "# comment\nalice report "+tokA+"\nbob query "+tokB+"\n")
	var s Store
	if err := s.Load(p); err != nil {
		t.Fatal(err)
	}
	if id, ok := s.Lookup(tokA); !ok || id.Name != "alice" || id.Scope != ScopeReport {
		t.Fatalf("alice: %+v %v", id, ok)
	}
	if id, ok := s.Lookup(tokB); !ok || id.Scope != ScopeQuery {
		t.Fatalf("bob: %+v %v", id, ok)
	}
	for _, bad := range []string{"", "nope", tokA + "x", tokA[:10]} {
		if _, ok := s.Lookup(bad); ok {
			t.Fatalf("%q must not authenticate", bad)
		}
	}

	if err := os.WriteFile(p, []byte("bob query "+tokB+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Load(p); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Lookup(tokA); ok {
		t.Fatal("a revoked token must stop working after reload")
	}
}

func TestFailedReloadKeepsPreviousSet(t *testing.T) {
	p := write(t, "alice report "+tokA+"\n")
	var s Store
	if err := s.Load(p); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("alice report short\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Load(p); err == nil {
		t.Fatal("a short token must be rejected")
	}
	if _, ok := s.Lookup(tokA); !ok {
		t.Fatal("previous tokens must survive a failed reload")
	}
}

func TestParseRules(t *testing.T) {
	cases := map[string]string{
		"short token":    "a report abc\n",
		"bad scope":      "a admin " + tokA + "\n",
		"too few fields": "a report\n",
		"duplicate name": "a report " + tokA + "\na query " + tokB + "\n",
		"duplicate val":  "a report " + tokA + "\nb query " + tokA + "\n",
		"empty":          "# nothing\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parse(strings.NewReader(content)); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestErrorsNeverContainTokenValues(t *testing.T) {
	_, err := parse(strings.NewReader("a report " + tokA + "\nb query " + tokA + "\n"))
	if err == nil || strings.Contains(err.Error(), tokA) {
		t.Fatalf("error leaks the token: %v", err)
	}
}
