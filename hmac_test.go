package paylayer

import (
	"context"
	"encoding/hex"
	"testing"
	"time"
)

func TestHMACBackendIssueThenVerifyRoundTrips(t *testing.T) {
	b := NewHMACBackend([]byte("test-key"))
	tok, err := b.Issue(context.Background())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	ok, err := b.Verify(context.Background(), tok.Token)
	if err != nil || !ok {
		t.Errorf("Verify(issued token) = %v, %v, want true, nil", ok, err)
	}
}

func TestHMACBackendVerifyRejectsNeverIssuedToken(t *testing.T) {
	b := NewHMACBackend([]byte("test-key"))
	ok, err := b.Verify(context.Background(), "never-issued")
	if err != nil || ok {
		t.Errorf("Verify(garbage, not even valid hex) = %v, %v, want false, nil", ok, err)
	}
}

func TestHMACBackendVerifyRejectsExpiredToken(t *testing.T) {
	b := NewHMACBackend([]byte("test-key"))
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

func TestHMACBackendVerifyRejectsWrongKey(t *testing.T) {
	issuer := NewHMACBackend([]byte("issuer-key"))
	tok, err := issuer.Issue(context.Background())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	verifier := NewHMACBackend([]byte("a-completely-different-key"))
	ok, err := verifier.Verify(context.Background(), tok.Token)
	if err != nil || ok {
		t.Errorf("Verify(token signed by a different key) = %v, %v, want false, nil", ok, err)
	}
}

func TestHMACBackendVerifyRejectsTamperedToken(t *testing.T) {
	b := NewHMACBackend([]byte("test-key"))
	tok, err := b.Issue(context.Background())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	raw, err := hex.DecodeString(tok.Token)
	if err != nil {
		t.Fatalf("decoding issued token: %v", err)
	}
	raw[0] ^= 0xff // flip a bit in the nonce
	tampered := hex.EncodeToString(raw)

	ok, err := b.Verify(context.Background(), tampered)
	if err != nil || ok {
		t.Errorf("Verify(tampered token) = %v, %v, want false, nil", ok, err)
	}
}

func TestHMACBackendVerifyRejectsWrongLength(t *testing.T) {
	b := NewHMACBackend([]byte("test-key"))
	for _, tok := range []string{"", "ab", hex.EncodeToString([]byte("too short to be real"))} {
		ok, err := b.Verify(context.Background(), tok)
		if err != nil || ok {
			t.Errorf("Verify(%q) = %v, %v, want false, nil", tok, ok, err)
		}
	}
}

func TestHMACBackendIssueUsesDefaultTTLWhenZero(t *testing.T) {
	b := NewHMACBackend([]byte("test-key"))
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

func TestHMACBackendIssueProducesDistinctTokens(t *testing.T) {
	b := NewHMACBackend([]byte("test-key"))
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

// TestHMACBackendSeparateInstancesSameKeyInteroperate covers the real
// property EphemNet's own D012 depends on: the issuer and verifier
// don't have to be the literal same *HMACBackend value, only the same
// key — this is what "no key distribution needed as long as both run
// in one process" actually rests on at the code level, even though in
// practice EphemNet shares one instance for both.
func TestHMACBackendSeparateInstancesSameKeyInteroperate(t *testing.T) {
	issuer := NewHMACBackend([]byte("shared-key"))
	verifier := NewHMACBackend([]byte("shared-key"))

	tok, err := issuer.Issue(context.Background())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	ok, err := verifier.Verify(context.Background(), tok.Token)
	if err != nil || !ok {
		t.Errorf("Verify(token from a separate instance with the same key) = %v, %v, want true, nil", ok, err)
	}
}
