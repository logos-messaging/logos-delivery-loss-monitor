// Package auth stores report and query tokens as SHA-256 digests and looks them up in constant time.
package auth

import (
	"bufio"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
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

// HashPrefix marks a token column that holds the hex SHA-256 of the token instead of the token.
const HashPrefix = "sha256:"

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

// parseDigest returns the SHA-256 digest for a token column: either "sha256:<64 hex>" or a plain token.
func parseDigest(col string) ([sha256.Size]byte, error) {
	var d [sha256.Size]byte
	if hexDigest, ok := strings.CutPrefix(col, HashPrefix); ok {
		b, err := hex.DecodeString(hexDigest)
		if err != nil || len(b) != sha256.Size {
			return d, fmt.Errorf("want %s followed by %d hex characters", HashPrefix, 2*sha256.Size)
		}
		copy(d[:], b)
		return d, nil
	}
	if len(col) < MinTokenLen {
		return d, fmt.Errorf("token is shorter than %d characters", MinTokenLen)
	}
	return sha256.Sum256([]byte(col)), nil
}

// parse reads "<name> <scope> <token>" lines, where <token> is a plain token or "sha256:<hex>".
// Blank lines and lines starting with # are ignored.
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
		d, err := parseDigest(tok)
		if err != nil {
			return nil, fmt.Errorf("tokens file line %d: token %q: %w", n, name, err)
		}
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

// Names returns the sorted names of the active tokens.
func (s *Store) Names() []string {
	es := s.entries.Load()
	if es == nil {
		return nil
	}
	names := make([]string, 0, len(*es))
	for i := range *es {
		names = append(names, (*es)[i].Name)
	}
	sort.Strings(names)
	return names
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
