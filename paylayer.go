// Package paylayer implements D012's payment middle layer: a contract
// deliberately agnostic to how payment was actually verified. A
// product's own write path only ever asks a Backend "is this token
// currently valid?" — it never sees macaroon, invoice, Lightning, or
// any other payment-mechanism-specific detail. That vocabulary is
// confined entirely to a real Backend's own implementation, one layer
// below this package. See plan/designs/D012-purchase-token-contract.md
// for the full design, open questions, and why the contract is shaped
// this way.
package paylayer

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// PurchaseToken is the entire contract's payload: an opaque value plus
// when it stops being valid. How it was obtained — a real payment, an
// operator-configured fixed value, anything else — is a Backend's own
// concern and never appears here.
type PurchaseToken struct {
	Token     string
	ExpiresAt time.Time
}

// Expired reports whether t is no longer valid at the given instant.
func (t PurchaseToken) Expired(now time.Time) bool {
	return !now.Before(t.ExpiresAt)
}

// ErrBackendUnavailable is returned by a Backend when it cannot answer
// at all (a real backend's own upstream is down, or — for Mock — a
// simulated hard failure). Distinct from a plain "invalid" result
// (Verify returning false, nil) so a caller can tell "this token is
// bad" apart from "the backend itself couldn't be reached" if it
// needs to — most callers should still treat both as "not authorized"
// for the request at hand.
var ErrBackendUnavailable = errors.New("paylayer: backend unavailable")

// Backend verifies whether a presented token is currently valid. How
// a token was actually earned is entirely the Backend's own concern
// and never appears in this interface — this is what makes swapping a
// Mock for a real, payment-backed implementation a configuration
// change rather than a code change on the calling side.
type Backend interface {
	// Verify reports whether token is currently valid. A caller must
	// treat a false result and a non-nil error identically for the
	// purposes of authorizing a request (this project's own no-leak
	// discipline — see CLAUDE.md — applies here too: don't let the
	// distinction leak into an observable response difference).
	Verify(ctx context.Context, token string) (bool, error)
}

// TokenHeader is the header a product checks for a presented
// PurchaseToken. Deliberately separate from cinder's own
// api.PaidInternalHeader (Aperture's network-boundary proof that a
// request came through it at all) — see D012 and D005 for why those
// are two different concerns that happen to compose, not one
// mechanism doing double duty. This header is the payment-layer
// concern; PaidInternalHeader is the transport/trust-boundary one.
const TokenHeader = "X-Purchase-Token"

// RequireToken wraps a handler so it 404s — not 401/403, matching this
// project's own no-leak instinct (a payment-gated listener shouldn't
// confirm its own existence to a caller that doesn't hold a valid
// token) — unless the request carries a TokenHeader value that
// backend.Verify confirms is currently valid.
func RequireToken(next http.Handler, backend Backend) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get(TokenHeader)
		if token == "" {
			http.NotFound(w, r)
			return
		}
		ok, err := backend.Verify(r.Context(), token)
		if err != nil || !ok {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}
