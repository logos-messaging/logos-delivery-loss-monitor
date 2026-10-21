package landing

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/logos-messaging/logos-delivery-loss-monitor/server/internal/config"
)

func testConfig(t *testing.T, kv ...string) *config.Config {
	t.Helper()
	m := map[string]string{
		"LOSSMON_TOKENS_FILE":    "/run/lossmon/tokens",
		"LOSSMON_PROBE_KEY_FILE": "/run/lossmon/probe_key",
		"LOSSMON_PUBLIC_URL":     "https://lossmon.example.org",
	}
	for i := 0; i+1 < len(kv); i += 2 {
		m["LOSSMON_"+kv[i]] = kv[i+1]
	}
	cfg, err := config.Load(func(k string) string { return m[k] })
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func render(t *testing.T, version string, kv ...string) string {
	t.Helper()
	body, err := Render(testConfig(t, kv...), version)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestLocationsAndPreset(t *testing.T) {
	body := render(t, "0.2.0", "PRESET", "logos.test", "LOCATIONS", "eu,na")
	for _, want := range []string{"--preset=logos.test", "<code>eu</code> <code>na</code>.", "--location=&lt;location&gt;"} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q", want)
		}
	}
	for _, unwanted := range []string{"<code>af</code>", "<code>oc</code>", "--allow-preset"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("body contains %q", unwanted)
		}
	}
}

func TestSingleLocationFilledIn(t *testing.T) {
	body := render(t, "0.2.0", "LOCATIONS", "eu")
	if !strings.Contains(body, "--location=eu") || strings.Contains(body, "&lt;location&gt;") {
		t.Fatalf("single location not filled in:\n%s", body)
	}
}

func TestCustomPresetAddsAllowPreset(t *testing.T) {
	body := render(t, "0.2.0", "PRESET", "status.prod", "CLUSTER_ID", "16", "ALLOW_PRESET", "status.prod")
	if !strings.Contains(body, "--preset=status.prod") || !strings.Contains(body, "--allow-preset=status.prod") {
		t.Fatalf("custom preset flags missing:\n%s", body)
	}
}

func TestNoSecretsOnPage(t *testing.T) {
	body := render(t, "0.2.0")
	for _, s := range []string{"0x", "probe_key", "tokens", "/run/lossmon", "Bearer"} {
		if strings.Contains(body, s) {
			t.Errorf("body contains %q", s)
		}
	}
}

func TestNoScriptAndStraightQuotes(t *testing.T) {
	body := render(t, "0.2.0")
	if strings.Contains(strings.ToLower(body), "<script") {
		t.Error("page contains a script")
	}
	for _, r := range []string{"‘", "’", "“", "”"} {
		if strings.Contains(body, r) {
			t.Errorf("page contains curly quote %q", r)
		}
	}
	if strings.Contains(body, "http://") {
		t.Error("page references a plain http URL")
	}
}

func TestVersionPinning(t *testing.T) {
	body := render(t, "0.2.0")
	for _, want := range []string{
		"releases/download/v0.2.0/install.sh | LOSSMON_VERSION=0.2.0 sh",
		"blob/v0.2.0/client/README.md#install",
		"lossmon-server 0.2.0",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q", want)
		}
	}
	if strings.Contains(body, "latest") {
		t.Error("release page falls back to latest")
	}

	body = render(t, "dev")
	if strings.Contains(body, "releases/download/v") || strings.Contains(body, "LOSSMON_VERSION") || strings.Contains(body, "lossmon-server dev") {
		t.Errorf("non-release page shows an install line or version:\n%s", body)
	}
	if !strings.Contains(body, "not a release build") {
		t.Error("non-release page lacks the fallback text")
	}
}

func TestIsRelease(t *testing.T) {
	for v, want := range map[string]bool{
		"0.2.0": true, "1.10.3": true, "0.2.0-rc.1": true,
		"": false, "dev": false, "v0.2.0": false, "0.2": false, "0.2.0+dirty": false, "0.2.0-$(id)": false, "0.2.0\n": false,
	} {
		if got := IsRelease(v); got != want {
			t.Errorf("IsRelease(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestServerURL(t *testing.T) {
	if body := render(t, "0.2.0"); !strings.Contains(body, "--server-url=https://lossmon.example.org \\") {
		t.Errorf("public URL missing:\n%s", body)
	}
	body := render(t, "0.2.0", "PUBLIC_URL", "")
	if !strings.Contains(body, "--server-url=https://&lt;server&gt; \\") {
		t.Errorf("placeholder URL missing:\n%s", body)
	}
}

func TestBodySize(t *testing.T) {
	if n := len(render(t, "0.2.0")); n >= 16<<10 {
		t.Fatalf("body is %d bytes", n)
	}
}

func next() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Next", "1")
		w.WriteHeader(http.StatusTeapot)
	})
}

func TestMountHeadersAndBody(t *testing.T) {
	body := []byte(render(t, "0.2.0"))
	h := Mount(next(), body)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/?x=1", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != string(body) {
		t.Fatalf("status %d, body mismatch", rec.Code)
	}
	want := map[string]string{
		"Content-Type":            "text/html; charset=utf-8",
		"Content-Security-Policy": "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'",
		"X-Content-Type-Options":  "nosniff",
		"Referrer-Policy":         "no-referrer",
		"Cache-Control":           "public, max-age=300",
	}
	for k, v := range want {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if got := rec.Header().Get("Content-Length"); got == "" {
		t.Error("Content-Length missing")
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("HEAD / status %d", rec.Code)
	}
}

func TestMountIgnoresHost(t *testing.T) {
	h := Mount(next(), []byte(render(t, "0.2.0")))
	get := func(host string) string {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Host = host
		req.Header.Set("X-Forwarded-Host", host)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Body.String()
	}
	if get("lossmon.example.org") != get("evil.example") {
		t.Fatal("body depends on the Host header")
	}
}

func TestMountRouting(t *testing.T) {
	h := Mount(next(), []byte("page"))
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/"}, {http.MethodOptions, "/"}, {http.MethodGet, "/foo"},
		{http.MethodGet, "/index.html"}, {http.MethodGet, "/v1/x"}, {http.MethodPost, "/v1/register"},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if rec.Code != http.StatusTeapot || rec.Header().Get("X-Next") != "1" {
			t.Errorf("%s %s: status %d, did not reach next", tc.method, tc.path, rec.Code)
		}
	}
}
