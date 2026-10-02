package paylayer

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"time"
)

// hmacNonceSize/hmacExpirySize/hmacSigSize are the three pieces
// concatenated on the wire — see HMACBackend's own doc comment for
// why: hmacNonceSize random bytes making every issued token distinct
// even at the same expiry, followed by an 8-byte big-endian Unix
// expiry, followed by an HMAC-SHA256 signature over both.
const (
	hmacNonceSize  = 16
	hmacExpirySize = 8
	hmacSigSize    = sha256.Size
)

// HMACBackend is a self-verifying Backend: Issue mints a token whose
// own bytes carry everything Verify needs (a random nonce, an expiry,
// and an HMAC-SHA256 signature over both) — no network call, no
// shared database, the same cost profile StaticBackend already has,
// while still proving whoever presents a token is holding something
// only Issue's own caller could have produced (the same signing key).
// Same self-verifying-receipt shape persona's own A004 already uses
// for its attestation-cost gateway — reused, not reinvented. See
// EphemNet's own plan/designs/D012-real-lightning-backed-paylayer-backend.md
// for the full design history this implements.
//
// paylayer itself never decides who's allowed to call Issue — that's
// entirely up to whatever fronts it (EphemNet's own case: a thin HTTP
// handler behind a self-hosted Aperture instance, gating Issue on a
// real Lightning payment; this package has no Lightning/Aperture
// vocabulary at all, matching every other Backend here).
type HMACBackend struct {
	key []byte

	// TTL is how long a newly issued token stays valid. Zero means
	// DefaultTTL (the same constant Mock uses).
	TTL time.Duration
}

// NewHMACBackend returns an HMACBackend signing/verifying with key.
// key is this backend's entire trust boundary — generate it with
// something like `openssl rand -hex 32` and treat it exactly like any
// other secret a consumer already handles as a plain operator-
// provided value. The same key must be used for both Issue and
// Verify — normally one process, one instance, sharing one key in
// memory; see HMACBackend's own doc comment for why that's not a key-
// distribution problem for a consumer running both in one process.
func NewHMACBackend(key []byte) *HMACBackend {
	return &HMACBackend{key: key}
}

// Issue mints a new self-verifying PurchaseToken — see this type's
// own doc comment for the wire format.
func (b *HMACBackend) Issue(ctx context.Context) (PurchaseToken, error) {
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
	sig := b.sign(payload)

	raw := append(payload, sig...)
	return PurchaseToken{Token: hex.EncodeToString(raw), ExpiresAt: expiresAt}, nil
}

// Verify reports whether token is a currently-valid signature this
// backend's own key produced — per the Backend interface, no network
// call or shared state involved either way. A malformed token (wrong
// length, not valid hex, tampered bytes) is indistinguishable from an
// expired or never-issued one — all three just mean "not authorized,"
// matching this package's own no-leak discipline.
func (b *HMACBackend) Verify(ctx context.Context, token string) (bool, error) {
	raw, err := hex.DecodeString(token)
	if err != nil || len(raw) != hmacNonceSize+hmacExpirySize+hmacSigSize {
		return false, nil
	}
	payload := raw[:hmacNonceSize+hmacExpirySize]
	sig := raw[hmacNonceSize+hmacExpirySize:]

	if subtle.ConstantTimeCompare(sig, b.sign(payload)) != 1 {
		return false, nil
	}
	return time.Now().Before(decodeExpiry(payload)), nil
}

func (b *HMACBackend) sign(payload []byte) []byte {
	mac := hmac.New(sha256.New, b.key)
	mac.Write(payload)
	return mac.Sum(nil)
}

// encodePayload/decodeExpiry are free functions, not methods — they
// don't touch the signing key, only the nonce/expiry layout, which a
// test can exercise directly without needing a real HMACBackend.
func encodePayload(nonce []byte, expiresAt time.Time) []byte {
	payload := make([]byte, hmacNonceSize+hmacExpirySize)
	copy(payload, nonce)
	binary.BigEndian.PutUint64(payload[hmacNonceSize:], uint64(expiresAt.Unix()))
	return payload
}

func decodeExpiry(payload []byte) time.Time {
	sec := binary.BigEndian.Uint64(payload[hmacNonceSize:])
	return time.Unix(int64(sec), 0)
}
