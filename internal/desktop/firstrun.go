package desktop

import (
	"sync"

	"github.com/coste360/porto/internal/crypto"
)

// firstRunToken is the single-use token of the first run (m6a-plan §2.6): 32 random bytes, kept in memory as a
// hash only, valid until used or the process exits. It is handed to the window in the URL fragment, so it never
// appears in a request line or an access log.
type firstRunToken struct {
	mu   sync.Mutex
	hash string
	used bool
	busy bool
}

// mint creates a new token and returns it (the only time it exists in clear).
func (t *firstRunToken) mint() (string, error) {
	tok, err := crypto.RandomToken(32)
	if err != nil {
		return "", err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.hash, t.used, t.busy = crypto.SHA256Hex(tok), false, false
	return tok, nil
}

func (t *firstRunToken) matches(tok string) bool {
	return t.hash != "" && tok != "" && crypto.ConstantTimeEqual(crypto.SHA256Hex(tok), t.hash)
}

// begin reserves the token for one attempt (no other attempt runs meanwhile). finish(true) burns it,
// finish(false) makes it usable again (e.g. the form had a validation error).
func (t *firstRunToken) begin(tok string) (finish func(ok bool), ok bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.used || t.busy || !t.matches(tok) {
		return nil, false
	}
	t.busy = true
	return func(done bool) {
		t.mu.Lock()
		defer t.mu.Unlock()
		t.busy = false
		if done {
			t.used = true
		}
	}, true
}

// pending reports whether a token was minted and not used.
func (t *firstRunToken) pending() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.hash != "" && !t.used
}
