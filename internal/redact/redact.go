// Package redact removes secret values from text yahiko prints.
//
// yahiko never writes the environment into a report, but a failing command's
// standard error or an error message can still carry a secret the command was
// given. Every such text passes through a Redactor first, which replaces the
// values of secret-looking environment variables with "***".
package redact

import (
	"regexp"
	"sort"
	"strings"
	"sync"
)

// secretName matches the names of environment variables whose values are
// treated as secrets.
var secretName = regexp.MustCompile(`(?i)(TOKEN|SECRET|PASSWORD|PASSWD|PASSPHRASE|CREDENTIAL|PRIVATE|API_?KEY|ACCESS_?KEY|AUTH|COOKIE|SESSION)`)

// minSecretLen is the shortest value treated as a secret. Shorter values
// ("1", "true") would redact unrelated text everywhere.
const minSecretLen = 6

// Mask replaces a secret value.
const Mask = "***"

// IsSecretName reports whether an environment variable name looks like it
// holds a secret.
func IsSecretName(name string) bool {
	return secretName.MatchString(name)
}

// Redactor replaces known secret values. It is safe for concurrent use.
type Redactor struct {
	mu     sync.RWMutex
	values []string
	seen   map[string]bool
}

// New builds a Redactor from KEY=VALUE environment entries (os.Environ form)
// plus any extra values known to be secret.
func New(environ []string, extra ...string) *Redactor {
	r := &Redactor{seen: map[string]bool{}}
	for _, kv := range environ {
		k, v, ok := strings.Cut(kv, "=")
		if ok && IsSecretName(k) {
			r.Add(v)
		}
	}
	for _, v := range extra {
		r.Add(v)
	}
	return r
}

// Add registers another secret value, such as one a suite passes to a command
// under a secret-looking name.
func (r *Redactor) Add(v string) {
	if r == nil || len(v) < minSecretLen {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.seen[v] {
		return
	}
	r.seen[v] = true
	r.values = append(r.values, v)
	// Longest first, so a secret containing another secret is masked whole.
	sort.Slice(r.values, func(i, j int) bool { return len(r.values[i]) > len(r.values[j]) })
}

// MaxLen returns the length of the longest known secret.
func (r *Redactor) MaxLen() int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.values) == 0 {
		return 0
	}
	return len(r.values[0])
}

// String masks every known secret in s.
func (r *Redactor) String(s string) string {
	if r == nil {
		return s
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, v := range r.values {
		s = strings.ReplaceAll(s, v, Mask)
	}
	return s
}
