// Package auth stores report and query tokens as SHA-256 digests and looks them up in constant time.
package auth

import (
	"bufio"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync/atomic"
)

// Scope is the permission a token carries.
type Scope string

const (
	// ScopeReport allows registering sessions and posting records.
	ScopeReport Scope = "report"
	// ScopeQuery allows reading stats.
	ScopeQuery Scope = "query"
)

// MinTokenLen is the shortest accepted token.
const MinTokenLen = 32

// Identity is the authenticated caller. Name is the only token-derived value that may be logged.
type Identity struct {
	Name  string
	Scope Scope
}

type entry struct {
	Identity
	digest [sha256.Size]byte
}

// Store holds the active token set and replaces it atomically on reload.
type Store struct {
	entries atomic.Pointer[[]entry]
}

// parse reads "<name> <scope> <token>" lines. Blank lines and lines starting with # are ignored.
func parse(r io.Reader) ([]entry, error) {
	var out []entry
	names, digests := map[string]bool{}, map[[sha256.Size]byte]bool{}
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 3 {
			return nil, fmt.Errorf("tokens file line %d: want \"<name> <scope> <token>\"", n)
		}
		name, scope, tok := f[0], Scope(f[1]), f[2]
		if scope != ScopeReport && scope != ScopeQuery {
			return nil, fmt.Errorf("tokens file line %d: unknown scope", n)
		}
		if len(tok) < MinTokenLen {
			return nil, fmt.Errorf("tokens file line %d: token %q is shorter than %d characters", n, name, MinTokenLen)
		}
		d := sha256.Sum256([]byte(tok))
		if names[name] {
			return nil, fmt.Errorf("tokens file line %d: duplicate token name %q", n, name)
		}
		if digests[d] {
			return nil, fmt.Errorf("tokens file line %d: token %q duplicates another token value", n, name)
		}
		names[name], digests[d] = true, true
		out = append(out, entry{Identity: Identity{Name: name, Scope: scope}, digest: d})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, errors.New("tokens file has no tokens")
	}
	return out, nil
}

// Load parses the tokens file at path and installs it.
func (s *Store) Load(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("tokens file: %w", err)
	}
	defer f.Close()
	es, err := parse(f)
	if err != nil {
		return err
	}
	s.entries.Store(&es)
	return nil
}

// Lookup returns the identity for a presented token. It scans every entry so timing does not depend on the match position.
func (s *Store) Lookup(token string) (Identity, bool) {
	es := s.entries.Load()
	if es == nil {
		return Identity{}, false
	}
	d := sha256.Sum256([]byte(token))
	var found Identity
	var ok int
	for i := range *es {
		m := subtle.ConstantTimeCompare(d[:], (*es)[i].digest[:])
		if m == 1 {
			found = (*es)[i].Identity
		}
		ok |= m
	}
	return found, ok == 1
}
