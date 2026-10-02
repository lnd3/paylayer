package paylayer

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// Issuer mints a new PurchaseToken. Deliberately separate from
// Backend (which only verifies) — StaticBackend has no concept of
// issuing anything, while Mock and HMACBackend both do. A product
// wires whatever actually gates issuance (payment confirmation,
// Aperture's own L402 challenge, anything else) in front of
// IssueHandler below; this interface and that handler have no
// payment-mechanism vocabulary of their own, matching every other
// contract in this package.
type Issuer interface {
	Issue(ctx context.Context) (PurchaseToken, error)
}

// issueResponse is IssueHandler's own response body — just enough for
// a caller to actually use the token: the value and when it stops
// being valid. No other PurchaseToken field exists today, but this is
// its own type (not PurchaseToken re-encoded blindly) so adding an
// internal-only field to PurchaseToken later doesn't silently leak it
// over HTTP.
type issueResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// IssueHandler returns an http.Handler that calls issuer.Issue and
// returns the result as JSON — the generic "mint a token and hand it
// back" glue every real issuer-fronting service needs, regardless of
// what actually gates the call (see EphemNet's own D012: a thin
// handler like this one, behind a self-hosted Aperture instance,
// gating on a real Lightning payment). This handler itself does
// nothing payment-related at all — whatever sits in front of it
// (Aperture, a test harness, anything) is what decides whether a
// given request is even allowed to reach it.
//
// Only responds to POST — issuing a token is a real side effect
// (Mock/HMACBackend both consume randomness and, for a different
// future Backend, could consume a real payment confirmation), not an
// idempotent GET.
func IssueHandler(issuer Issuer) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		tok, err := issuer.Issue(r.Context())
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(issueResponse{
			Token:     tok.Token,
			ExpiresAt: tok.ExpiresAt,
		})
	})
}
