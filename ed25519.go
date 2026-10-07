package paylayer

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"
)

// ed25519SigSize is the one piece of the wire shape that actually
// differs from HMACBackend's own — the nonce/expiry layout is
// identical (hmacNonceSize/hmacExpirySize, reused directly rather
// than redeclared under a new name), only the signature mechanism and
// its size change.
const ed25519SigSize = ed25519.SignatureSize

// ErrNoPrivateKey is returned by Issue on an Ed25519Backend
// constructed via NewEd25519VerifierBackend — that instance holds
// only the public half of the keypair, by design (see this type's
// own doc comment), so it can never mint a token.
var ErrNoPrivateKey = errors.New("paylayer: this Ed25519Backend has no private key — construct via NewEd25519IssuerBackend to mint tokens")

// Ed25519Backend is a self-verifying Backend+Issuer like HMACBackend
// — Issue mints a token whose own bytes carry a random nonce, an
// embedded expiry, and a signature over both; Verify checks the
// signature and expiry with no network call and no shared state —
// but asymmetric: Verify only ever needs the *public* half of the
// keypair, never the private half Issue needs. HMACBackend's own doc
// comment already explains why that distinction matters: its
// verification key IS its signing key, so no consumer can safely
// embed it client-side (anyone holding it could mint their own valid
// tokens too). This type exists for exactly the case that rules out —
// a distributed client, run somewhere this project doesn't control
// the deployment of at all (cinder's own cindertunnel, on a
// customer's machine, independently verifying a *peer's* license
// without any network call or anything that could forge one), can
// safely embed the public key at build time.
//
// Issue and Verify genuinely can run as two separate processes/
// binaries, each holding only its own half of the keypair — unlike
// HMACBackend, this IS a real key-distribution split, not an avoided
// one. The private key stays wherever Issue actually runs (normally
// the same server-side process already minting via HMACBackend/
// StripeBackend today, see MultiBackend for combining several);
// the public key ships in every verifying binary.
//
// Same wire-shape precedent as HMACBackend/StripeBackend — see
// EphemNet's own plan/designs/D012-real-lightning-backed-paylayer-backend.md
// for the design history this continues, extended for cinder's own
// A035 (join-side independent license verification against a peer,
// not just a product's own write path).
type Ed25519Backend struct {
	privateKey ed25519.PrivateKey // nil on a verifier-only instance
	publicKey  ed25519.PublicKey

	// TTL is how long a newly issued token stays valid. Zero means
	// DefaultTTL. Only meaningful on an issuing instance — Verify
	// reads the expiry from the token itself, never from this field.
	TTL time.Duration
}

// GenerateEd25519Keypair is a thin wrapper over the stdlib so a
// consumer never has to import crypto/ed25519 directly just to
// provision one — there's no first-class `openssl` workflow for this
// the way `openssl rand -hex 32` covers HMACBackend's own key.
// Generate once, keep privateKey exactly like any other secret
// (NewHMACBackend's own doc comment on key handling applies equally
// here); publicKey is free to embed/publish.
func GenerateEd25519Keypair() (publicKey ed25519.PublicKey, privateKey ed25519.PrivateKey, err error) {
	return ed25519.GenerateKey(rand.Reader)
}

// NewEd25519IssuerBackend returns a Backend+Issuer holding the real
// private key — for the same single-process shape HMACBackend's own
// doc comment describes as the common case (one server-side process,
// Issue gated behind a real payment flow, Verify also available
// locally if this same process ever needs it).
func NewEd25519IssuerBackend(privateKey ed25519.PrivateKey) *Ed25519Backend {
	return &Ed25519Backend{privateKey: privateKey, publicKey: privateKey.Public().(ed25519.PublicKey)}
}

// NewEd25519VerifierBackend returns a Backend that can only verify —
// for every other consumer, including ones this project doesn't
// control the deployment of at all. Issue on the result always
// returns ErrNoPrivateKey.
func NewEd25519VerifierBackend(publicKey ed25519.PublicKey) *Ed25519Backend {
	return &Ed25519Backend{publicKey: publicKey}
}

// Issue mints a new self-verifying PurchaseToken — see this type's
// own doc comment for the wire format. Errors with ErrNoPrivateKey on
// a verifier-only instance.
func (b *Ed25519Backend) Issue(ctx context.Context) (PurchaseToken, error) {
	if b.privateKey == nil {
		return PurchaseToken{}, ErrNoPrivateKey
	}
	nonce := make([]byte, hmacNonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return PurchaseToken{}, err
	}

	ttl := b.TTL
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	expiresAt := time.Now().Add(ttl)

	payload := encodePayload(nonce, expiresAt)
	sig := ed25519.Sign(b.privateKey, payload)

	raw := append(payload, sig...)
	return PurchaseToken{Token: hex.EncodeToString(raw), ExpiresAt: expiresAt}, nil
}

// Verify reports whether token carries a currently-valid signature
// from the matching private key — no network call or shared state
// either way, usable with only the public half of the keypair. A
// malformed token (wrong length, not valid hex, tampered bytes) is
// indistinguishable from an expired or never-issued one — all three
// just mean "not authorized," matching this package's own no-leak
// discipline.
func (b *Ed25519Backend) Verify(ctx context.Context, token string) (bool, error) {
	raw, err := hex.DecodeString(token)
	if err != nil || len(raw) != hmacNonceSize+hmacExpirySize+ed25519SigSize {
		return false, nil
	}
	payload := raw[:hmacNonceSize+hmacExpirySize]
	sig := raw[hmacNonceSize+hmacExpirySize:]

	if !ed25519.Verify(b.publicKey, payload, sig) {
		return false, nil
	}
	return time.Now().Before(decodeExpiry(payload)), nil
}
