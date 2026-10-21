// Package landing serves the static install and usage page at GET /.
package landing

import (
	"bytes"
	_ "embed"
	"html/template"
	"net/http"
	"regexp"
	"strconv"

	"github.com/logos-messaging/logos-delivery-loss-monitor/server/internal/config"
)

//go:embed page.html
var pageSource string

var page = template.Must(template.New("page").Parse(pageSource))

var releaseRe = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$`)

// view is the only data the template can read.
type view struct {
	PublicURL   string
	Release     bool
	Version     string
	Preset      string
	AllowPreset bool
	Locations   []string
	Location    string
}

// IsRelease reports whether version is a release semver: ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$
func IsRelease(version string) bool { return releaseRe.MatchString(version) }

// Render builds the landing page from cfg and the server build version. Called once at startup.
func Render(cfg *config.Config, version string) ([]byte, error) {
	v := view{
		PublicURL:   cfg.PublicURL,
		Release:     IsRelease(version),
		Preset:      cfg.Preset,
		AllowPreset: cfg.Preset != config.PresetLogosDev && cfg.Preset != config.PresetLogosTest,
		Locations:   cfg.Locations,
	}
	if v.Release {
		v.Version = version
	}
	if len(cfg.Locations) == 1 {
		v.Location = cfg.Locations[0]
	}
	var buf bytes.Buffer
	if err := page.Execute(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Mount serves body at exactly GET/HEAD / and passes every other request to next unchanged.
// body is shared read-only across requests and must not be modified after the call.
func Mount(next http.Handler, body []byte) http.Handler {
	length := strconv.Itoa(len(body))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		h := w.Header()
		h.Set("Content-Type", "text/html; charset=utf-8")
		h.Set("Content-Length", length)
		h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "public, max-age=300")
		_, _ = w.Write(body)
	})
	mux.Handle("/", next)
	return mux
}
