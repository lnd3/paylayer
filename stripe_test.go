package paylayer

import (
	"context"
	"encoding/hex"
	"errors"
	"testing"
	"time"
)

func TestStripeBackendIssueThenVerifyRoundTrips(t *testing.T) {
	b := NewStripeBackend([]byte("test-key"))
	tok, err := b.Issue(context.Background())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	ok, err := b.Verify(context.Background(), tok.Token)
	if err != nil || !ok {
		t.Errorf("Verify(issued token) = %v, %v, want true, nil", ok, err)
	}
}

func TestStripeBackendIssueQuantityIsOne(t *testing.T) {
	b := NewStripeBackend([]byte("test-key"))
	tok, err := b.Issue(context.Background())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	qty, ok := b.Quantity(tok.Token)
	if !ok || qty != 1 {
		t.Errorf("Quantity(flat-issued token) = %d, %v, want 1, true", qty, ok)
	}
}

func TestStripeBackendIssueTopUpRoundTripsQuantity(t *testing.T) {
	b := NewStripeBackend([]byte("test-key"))
	tok, err := b.IssueTopUp(context.Background(), 50)
	if err != nil {
		t.Fatalf("IssueTopUp: %v", err)
	}
	ok, err := b.Verify(context.Background(), tok.Token)
	if err != nil || !ok {
		t.Errorf("Verify(top-up token) = %v, %v, want true, nil", ok, err)
	}
	qty, ok := b.Quantity(tok.Token)
	if !ok || qty != 50 {
		t.Errorf("Quantity(top-up token) = %d, %v, want 50, true", qty, ok)
	}
}

func TestStripeBackendIssueTopUpRejectsNonPositiveQuantity(t *testing.T) {
	b := NewStripeBackend([]byte("test-key"))
	for _, qty := range []int{0, -1, -50} {
		_, err := b.IssueTopUp(context.Background(), qty)
		if !errors.Is(err, ErrInvalidQuantity) {
			t.Errorf("IssueTopUp(%d) error = %v, want ErrInvalidQuantity", qty, err)
		}
	}
}

func TestStripeBackendVerifyRejectsNeverIssuedToken(t *testing.T) {
	b := NewStripeBackend([]byte("test-key"))
	ok, err := b.Verify(context.Background(), "never-issued")
	if err != nil || ok {
		t.Errorf("Verify(garbage, not even valid hex) = %v, %v, want false, nil", ok, err)
	}
}

func TestStripeBackendVerifyRejectsExpiredToken(t *testing.T) {
	b := NewStripeBackend([]byte("test-key"))
	b.TTL = time.Millisecond
	tok, err := b.Issue(context.Background())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	ok, err := b.Verify(context.Background(), tok.Token)
	if err != nil || ok {
		t.Errorf("Verify(expired token) = %v, %v, want false, nil", ok, err)
	}
}

// TestStripeBackendQuantity_IgnoresExpiry covers the deliberate
// difference from Verify: Quantity answers "what does this signed
// receipt say," not "is it still usable" — a caller wanting both
// checks Verify too. Matters for reconciling a retried (Stripe's own
// at-least-once delivery), already-expired webhook.
func TestStripeBackendQuantity_IgnoresExpiry(t *testing.T) {
	b := NewStripeBackend([]byte("test-key"))
	b.TTL = time.Millisecond
	tok, err := b.IssueTopUp(context.Background(), 200)
	if err != nil {
		t.Fatalf("IssueTopUp: %v", err)
	}
	time.Sleep(5 * time.Millisecond)

	ok, err := b.Verify(context.Background(), tok.Token)
	if err != nil || ok {
		t.Fatalf("Verify(expired token) = %v, %v, want false, nil", ok, err)
	}
	qty, ok := b.Quantity(tok.Token)
	if !ok || qty != 200 {
		t.Errorf("Quantity(expired-but-signature-valid token) = %d, %v, want 200, true", qty, ok)
	}
}

func TestStripeBackendQuantityRejectsWrongKey(t *testing.T) {
	issuer := NewStripeBackend([]byte("issuer-key"))
	tok, err := issuer.IssueTopUp(context.Background(), 50)
	if err != nil {
		t.Fatalf("IssueTopUp: %v", err)
	}
	verifier := NewStripeBackend([]byte("a-completely-different-key"))
	qty, ok := verifier.Quantity(tok.Token)
	if ok || qty != 0 {
		t.Errorf("Quantity(token signed by a different key) = %d, %v, want 0, false", qty, ok)
	}
}

func TestStripeBackendQuantityRejectsTamperedQuantity(t *testing.T) {
	b := NewStripeBackend([]byte("test-key"))
	tok, err := b.IssueTopUp(context.Background(), 50)
	if err != nil {
		t.Fatalf("IssueTopUp: %v", err)
	}
	raw, err := hex.DecodeString(tok.Token)
	if err != nil {
		t.Fatalf("decoding issued token: %v", err)
	}
	// Flip a bit inside the quantity field itself, not the nonce —
	// proving the signature actually covers Quantity, not just
	// nonce+expiry.
	raw[stripeNonceSize+stripeExpirySize] ^= 0xff
	tampered := hex.EncodeToString(raw)

	ok, err := b.Verify(context.Background(), tampered)
	if err != nil || ok {
		t.Errorf("Verify(quantity-tampered token) = %v, %v, want false, nil", ok, err)
	}
	qty, ok := b.Quantity(tampered)
	if ok || qty != 0 {
		t.Errorf("Quantity(quantity-tampered token) = %d, %v, want 0, false", qty, ok)
	}
}

func TestStripeBackendVerifyRejectsWrongLength(t *testing.T) {
	b := NewStripeBackend([]byte("test-key"))
	for _, tok := range []string{"", "ab", hex.EncodeToString([]byte("too short to be real"))} {
		ok, err := b.Verify(context.Background(), tok)
		if err != nil || ok {
			t.Errorf("Verify(%q) = %v, %v, want false, nil", tok, ok, err)
		}
	}
}

func TestStripeBackendIssueUsesDefaultTTLWhenZero(t *testing.T) {
	b := NewStripeBackend([]byte("test-key"))
	before := time.Now()
	tok, err := b.Issue(context.Background())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	want := before.Add(DefaultTTL)
	if tok.ExpiresAt.Before(want.Add(-time.Second)) || tok.ExpiresAt.After(want.Add(time.Second)) {
		t.Errorf("ExpiresAt = %v, want close to %v (DefaultTTL)", tok.ExpiresAt, want)
	}
}

func TestStripeBackendIssueProducesDistinctTokens(t *testing.T) {
	b := NewStripeBackend([]byte("test-key"))
	tok1, err := b.Issue(context.Background())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	tok2, err := b.Issue(context.Background())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if tok1.Token == tok2.Token {
		t.Error("two Issue calls produced the identical token — the random nonce isn't doing its job")
	}
}

// TestStripeBackendSeparateInstancesSameKeyInteroperate mirrors
// HMACBackend's own equivalent test — same property: a webhook
// handler (issuer) and a payment-gated listener (verifier) don't have
// to be the literal same *StripeBackend value, only the same key.
func TestStripeBackendSeparateInstancesSameKeyInteroperate(t *testing.T) {
	issuer := NewStripeBackend([]byte("shared-key"))
	verifier := NewStripeBackend([]byte("shared-key"))

	tok, err := issuer.IssueTopUp(context.Background(), 50)
	if err != nil {
		t.Fatalf("IssueTopUp: %v", err)
	}
	ok, err := verifier.Verify(context.Background(), tok.Token)
	if err != nil || !ok {
		t.Errorf("Verify(token from a separate instance with the same key) = %v, %v, want true, nil", ok, err)
	}
	qty, ok := verifier.Quantity(tok.Token)
	if !ok || qty != 50 {
		t.Errorf("Quantity(token from a separate instance with the same key) = %d, %v, want 50, true", qty, ok)
	}
}

// TestStripeBackendImplementsBackendAndIssuer is a compile-time-ish
// check (via explicit interface assignment) that StripeBackend
// actually satisfies both contracts it claims to — same shape
// HMACBackend's own equivalent assertion would take, catching an
// accidental signature drift immediately rather than via some
// unrelated caller's build failure later.
func TestStripeBackendImplementsBackendAndIssuer(t *testing.T) {
	var _ Backend = (*StripeBackend)(nil)
	var _ Issuer = (*StripeBackend)(nil)
}
