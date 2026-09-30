package paylayer

import (
	"context"
	"crypto/subtle"
)

// StaticBackend is a Backend whose validity check is a single, fixed,
// operator-configured value with no expiry. This is cinder's own
// original paid-listener check (predating this package) generalized
// into the Backend interface rather than replaced — the reference
// "what today's already-verified-live production behavior looks like
// as a pluggable Backend," and a reasonable real backend for any
// product not yet ready to plug in genuine payment verification.
type StaticBackend struct {
	secret []byte
}

// NewStaticBackend returns a Backend that accepts exactly one fixed
// token value, compared in constant time.
func NewStaticBackend(secret string) *StaticBackend {
	return &StaticBackend{secret: []byte(secret)}
}

// Verify reports whether token matches the configured secret exactly.
// A length mismatch is already a definitive "no" and doesn't need a
// timing-safe comparison (an attacker learns nothing they couldn't
// already see from the token they sent) — same reasoning cinder's
// original check applied.
func (b *StaticBackend) Verify(ctx context.Context, token string) (bool, error) {
	got := []byte(token)
	if len(got) != len(b.secret) || subtle.ConstantTimeCompare(got, b.secret) != 1 {
		return false, nil
	}
	return true, nil
}
