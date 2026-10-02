package paylayer

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"time"
)

// stripeNonceSize/stripeExpirySize/stripeQuantitySize/stripeSigSize
// are the four pieces concatenated on the wire — same self-verifying
// shape HMACBackend already uses (a random nonce, an expiry, an
// HMAC-SHA256 signature), plus one addition: a 4-byte big-endian
// Quantity, signed in alongside the rest. See StripeBackend's own doc
// comment for why a plain valid/invalid receipt (HMACBackend's whole
// contract) isn't quite enough here.
const (
	stripeNonceSize     = 16
	stripeExpirySize    = 8
	stripeQuantitySize  = 4
	stripeSigSize       = sha256.Size
	stripeTokenRawSize  = stripeNonceSize + stripeExpirySize + stripeQuantitySize + stripeSigSize
	stripePayloadLength = stripeNonceSize + stripeExpirySize + stripeQuantitySize
)

// ErrInvalidQuantity is returned by IssueTopUp for a non-positive
// quantity — a top-up of zero or negative units isn't a real purchase
// and isn't something this backend will silently mint a token for.
var ErrInvalidQuantity = errors.New("paylayer: quantity must be positive")

// StripeBackend is a self-verifying Backend+Issuer for Stripe-gated
// purchases — EphemNet's own D012 interim/parallel billing rail
// alongside its Lightning-backed HMACBackend, not a replacement for
// it (either can issue a token for the same product; a listener
// gated by RequireToken can't tell which payment method produced the
// token it's holding, by design). Mechanically near-identical to
// HMACBackend (same nonce+expiry+HMAC-SHA256 wire shape, no network
// call, no shared database), with one real addition: every token also
// carries a signed Quantity, letting a caller recover not just "was
// this purchase valid" but "how many units were purchased" — needed
// for a metered product (EphemNet's own ephemnet-relay, sold in
// predefined top-up packages, e.g. 50GB increments) where a flat
// yes/no isn't enough information to credit the right amount to an
// internal usage ledger. A flat, non-metered purchase (e.g.
// ephemnet-domain's own annual subscription) just uses Issue, which
// mints a Quantity of 1 — meaningless on its own, just a consistent
// wire format rather than a second code path.
//
// Deliberately contains zero Stripe-specific vocabulary (no webhook
// signature verification, no price IDs, no Checkout Session parsing,
// no Stripe SDK dependency at all) — same reasoning HMACBackend has
// no Lightning/Aperture vocabulary. Whatever fronts this (a product's
// own Stripe webhook handler, verifying the webhook's own signature
// and deciding which predefined package's worth of Quantity to pass
// to IssueTopUp) is entirely that product's concern, confined outside
// this package.
type StripeBackend struct {
	key []byte

	// TTL is how long a newly issued token stays valid before it must
	// be redeemed — not the purchased product's own duration (a
	// year's subscription, 50GB of relay traffic), just how long the
	// receipt itself is good for. Zero means DefaultTTL.
	TTL time.Duration
}

// NewStripeBackend returns a StripeBackend signing/verifying with
// key — same trust-boundary reasoning as NewHMACBackend: generate it
// with something like `openssl rand -hex 32`, and use a key distinct
// from any HMACBackend's own if the two payment channels' blast radii
// should stay separate (compromising one can't forge the other's
// tokens) — sharing one key/instance across both is also a valid,
// simpler choice if that separation isn't wanted; this package takes
// no position on which.
func NewStripeBackend(key []byte) *StripeBackend {
	return &StripeBackend{key: key}
}

// Issue mints a flat token (Quantity 1) — for a non-metered purchase
// like an annual subscription, where there's nothing to count beyond
// "a valid purchase happened." Satisfies the Issuer interface.
func (b *StripeBackend) Issue(ctx context.Context) (PurchaseToken, error) {
	return b.IssueTopUp(ctx, 1)
}

// IssueTopUp mints a token attesting quantity units were purchased —
// for a metered product's own predefined top-up packages (e.g. a
// Stripe webhook handler that looked up a price ID in its own
// fixed price-id-to-GB table and calls this with that package's own
// size). quantity must be positive.
func (b *StripeBackend) IssueTopUp(ctx context.Context, quantity int) (PurchaseToken, error) {
	if quantity <= 0 {
		return PurchaseToken{}, ErrInvalidQuantity
	}

	nonce := make([]byte, stripeNonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return PurchaseToken{}, err
	}

	ttl := b.TTL
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	expiresAt := time.Now().Add(ttl)

	payload := encodeStripePayload(nonce, expiresAt, quantity)
	sig := b.sign(payload)

	raw := append(payload, sig...)
	return PurchaseToken{Token: hex.EncodeToString(raw), ExpiresAt: expiresAt}, nil
}

// Verify reports whether token is a currently-valid signature this
// backend's own key produced — per the Backend interface, no network
// call or shared state involved either way, same no-leak reasoning
// HMACBackend.Verify already documents (malformed, expired, and
// never-issued are all indistinguishably "not authorized").
func (b *StripeBackend) Verify(ctx context.Context, token string) (bool, error) {
	payload, ok := b.decodeAndCheckSig(token)
	if !ok {
		return false, nil
	}
	return time.Now().Before(decodeStripeExpiry(payload)), nil
}

// Quantity reports how many units token attests were purchased — only
// meaningful for a token this same backend's key actually signed;
// returns false for anything that doesn't check out, re-verifying the
// signature itself rather than trusting a caller to have already
// called Verify (a quantity decoded from an unverified token is worse
// than useless — it's a number an attacker chose). Does NOT check
// expiry — a caller wanting "is this still valid AND how much" should
// check Verify too; Quantity alone answers only "what does this
// signed receipt say," matching Stripe webhooks' own at-least-once
// delivery (a retried, already-expired webhook can still have its
// original quantity read back for reconciliation/logging even after
// the token itself would no longer authorize a live request).
func (b *StripeBackend) Quantity(token string) (int, bool) {
	payload, ok := b.decodeAndCheckSig(token)
	if !ok {
		return 0, false
	}
	return decodeStripeQuantity(payload), true
}

func (b *StripeBackend) decodeAndCheckSig(token string) (payload []byte, ok bool) {
	raw, err := hex.DecodeString(token)
	if err != nil || len(raw) != stripeTokenRawSize {
		return nil, false
	}
	payload = raw[:stripePayloadLength]
	sig := raw[stripePayloadLength:]
	if subtle.ConstantTimeCompare(sig, b.sign(payload)) != 1 {
		return nil, false
	}
	return payload, true
}

func (b *StripeBackend) sign(payload []byte) []byte {
	mac := hmac.New(sha256.New, b.key)
	mac.Write(payload)
	return mac.Sum(nil)
}

// encodeStripePayload/decodeStripeExpiry/decodeStripeQuantity are free
// functions, not methods — same reasoning HMACBackend's own
// encodePayload/decodeExpiry give: they don't touch the signing key,
// only the nonce/expiry/quantity layout, which a test can exercise
// directly without needing a real StripeBackend.
func encodeStripePayload(nonce []byte, expiresAt time.Time, quantity int) []byte {
	payload := make([]byte, stripePayloadLength)
	copy(payload, nonce)
	binary.BigEndian.PutUint64(payload[stripeNonceSize:], uint64(expiresAt.Unix()))
	binary.BigEndian.PutUint32(payload[stripeNonceSize+stripeExpirySize:], uint32(quantity))
	return payload
}

func decodeStripeExpiry(payload []byte) time.Time {
	sec := binary.BigEndian.Uint64(payload[stripeNonceSize:])
	return time.Unix(int64(sec), 0)
}

func decodeStripeQuantity(payload []byte) int {
	return int(binary.BigEndian.Uint32(payload[stripeNonceSize+stripeExpirySize:]))
}
