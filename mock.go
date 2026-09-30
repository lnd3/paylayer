package paylayer

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	mathrand "math/rand"
	"sync"
	"time"
)

// DefaultTTL is used when Mock.TTL is zero.
const DefaultTTL = time.Hour

// FailureMode selects what Mock simulates instead of a normal
// success. See D012's own "Mock backend" section: a mock that always
// succeeds instantly only ever proves a product's happy path.
type FailureMode int

const (
	// FailureNone performs no fault injection — the default.
	FailureNone FailureMode = iota
	// FailureTimeout blocks until the caller's own context is done,
	// then returns its error — simulating a backend that never
	// responds, so a caller's own timeout/deadline handling is
	// actually exercised rather than assumed correct.
	FailureTimeout
	// FailureHardFail returns ErrBackendUnavailable immediately (after
	// any configured Latency) — simulating a backend that is simply
	// down, as opposed to slow.
	FailureHardFail
	// FailureMalformed simulates an inconsistent backend response:
	// Issue hands back a token that will never actually verify (a
	// caller must not trust Issue's own return value blindly), and
	// Verify reports a token valid regardless of whether it was ever
	// issued (a caller relying solely on Verify's bool return, with no
	// independent check, is exposed to this — deliberately, since a
	// real backend could misbehave the same way).
	FailureMalformed
	// FailurePrematureExpiry mints a token that is already expired the
	// instant Issue returns it — a real edge case under clock skew or
	// slow delivery, which a caller's own expiry check must still
	// reject regardless of how it came to be expired.
	FailurePrematureExpiry
)

// Mock is a Backend requiring no real payment — for local development
// and tests. Deterministic and seeded (not just "random"), so a
// failing test is reproducible rather than flaky.
type Mock struct {
	mu     sync.Mutex
	tokens map[string]time.Time // token -> expiry
	rng    *mathrand.Rand

	// TTL is how long a normally-issued token stays valid. Zero means
	// DefaultTTL.
	TTL time.Duration

	// Latency, if positive, is waited before every Issue/Verify call
	// completes — simulating real network delay. Ignored when
	// FailureMode is FailureTimeout, which has its own, much longer
	// wait by design (see FailureTimeout's own comment).
	Latency time.Duration

	// FailureMode, if not FailureNone, is what a call simulates instead
	// of behaving normally, subject to FailureRate below.
	FailureMode FailureMode

	// FailureRate is the probability (0.0–1.0) that a given call hits
	// FailureMode rather than behaving normally. Zero — the field's
	// natural default — is treated as 1.0 ("always"), not "never":
	// once a caller has set FailureMode at all, the useful default is
	// a targeted "assert we handle exactly this failure" test, not a
	// rate of zero silently doing nothing. Set FailureRate explicitly
	// for broader resilience/soak testing instead.
	FailureRate float64
}

// NewMock returns a Mock seeded for reproducible fault-injection
// decisions — the same seed produces the same sequence of
// fault/no-fault outcomes across runs.
func NewMock(seed int64) *Mock {
	return &Mock{
		tokens: make(map[string]time.Time),
		rng:    mathrand.New(mathrand.NewSource(seed)),
	}
}

// Issue mints a new PurchaseToken with no real payment involved.
// Subject to the same fault injection as Verify — a real backend's
// issuing step can fail exactly like its verification step can.
func (m *Mock) Issue(ctx context.Context) (PurchaseToken, error) {
	fault := m.decideFault()
	if err := m.simulateLatency(ctx, fault); err != nil {
		return PurchaseToken{}, err
	}
	if fault == FailureHardFail {
		return PurchaseToken{}, ErrBackendUnavailable
	}

	raw := make([]byte, 16)
	if _, err := cryptorand.Read(raw); err != nil {
		return PurchaseToken{}, err
	}
	token := hex.EncodeToString(raw)

	ttl := m.TTL
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	expiresAt := time.Now().Add(ttl)
	if fault == FailurePrematureExpiry {
		expiresAt = time.Now().Add(-time.Second)
	}

	m.mu.Lock()
	m.tokens[token] = expiresAt
	m.mu.Unlock()

	if fault == FailureMalformed {
		// Hand back a token that was never actually recorded above —
		// it will never verify. See FailureMalformed's own comment.
		return PurchaseToken{Token: token + "-corrupted", ExpiresAt: expiresAt}, nil
	}
	return PurchaseToken{Token: token, ExpiresAt: expiresAt}, nil
}

// Verify reports whether token is currently valid, per the Backend
// interface.
func (m *Mock) Verify(ctx context.Context, token string) (bool, error) {
	fault := m.decideFault()
	if err := m.simulateLatency(ctx, fault); err != nil {
		return false, err
	}
	switch fault {
	case FailureHardFail:
		return false, ErrBackendUnavailable
	case FailureMalformed:
		return true, nil
	}

	m.mu.Lock()
	expiresAt, ok := m.tokens[token]
	m.mu.Unlock()
	if !ok {
		return false, nil
	}
	return time.Now().Before(expiresAt), nil
}

// decideFault rolls whether this call should simulate FailureMode, per
// FailureRate's own documented default.
func (m *Mock) decideFault() FailureMode {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailureMode == FailureNone {
		return FailureNone
	}
	rate := m.FailureRate
	if rate == 0 {
		rate = 1.0
	}
	if m.rng.Float64() < rate {
		return m.FailureMode
	}
	return FailureNone
}

// simulateLatency waits Latency (or, for FailureTimeout, a much
// longer duration meant to outlast any reasonable caller deadline),
// returning ctx.Err() if ctx ends first — which is the whole point of
// FailureTimeout: the caller's own deadline should win the race, not
// this function's own sleep duration.
func (m *Mock) simulateLatency(ctx context.Context, fault FailureMode) error {
	delay := m.Latency
	if fault == FailureTimeout {
		delay = time.Hour
	}
	if delay <= 0 {
		return ctx.Err()
	}
	select {
	case <-time.After(delay):
		return ctx.Err()
	case <-ctx.Done():
		return ctx.Err()
	}
}
