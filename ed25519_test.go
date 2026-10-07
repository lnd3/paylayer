package paylayer

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"testing"
	"time"
)

func TestEd25519BackendIssueThenVerifyRoundTrips(t *testing.T) {
	pub, priv, err := GenerateEd25519Keypair()
	if err != nil {
		t.Fatalf("GenerateEd25519Keypair: %v", err)
	}
	issuer := NewEd25519IssuerBackend(priv)
	tok, err := issuer.Issue(context.Background())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	verifier := NewEd25519VerifierBackend(pub)
	ok, err := verifier.Verify(context.Background(), tok.Token)
	if err != nil || !ok {
		t.Errorf("Verify(issued token) = %v, %v, want true, nil", ok, err)
	}
}

// TestEd25519BackendVerifierNeverNeedsThePrivateKey is the real point
// of this type: a verifier constructed from ONLY the public key —
// never having touched the private half at all — must still be able
// to confirm a token the issuer minted.
func TestEd25519BackendVerifierNeverNeedsThePrivateKey(t *testing.T) {
	pub, priv, err := GenerateEd25519Keypair()
	if err != nil {
		t.Fatalf("GenerateEd25519Keypair: %v", err)
	}
	issuer := NewEd25519IssuerBackend(priv)
	tok, err := issuer.Issue(context.Background())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	// A fresh verifier, built from a byte copy of the public key
	// alone — no shared Go value, no access to priv at all.
	pubCopy := make(ed25519.PublicKey, len(pub))
	copy(pubCopy, pub)
	verifier := NewEd25519VerifierBackend(pubCopy)

	ok, err := verifier.Verify(context.Background(), tok.Token)
	if err != nil || !ok {
		t.Errorf("Verify(token, public-key-only verifier) = %v, %v, want true, nil", ok, err)
	}
}

func TestEd25519BackendIssueOnVerifierOnlyInstanceErrors(t *testing.T) {
	pub, _, err := GenerateEd25519Keypair()
	if err != nil {
		t.Fatalf("GenerateEd25519Keypair: %v", err)
	}
	verifier := NewEd25519VerifierBackend(pub)
	_, err = verifier.Issue(context.Background())
	if err != ErrNoPrivateKey {
		t.Errorf("Issue on a verifier-only instance = %v, want ErrNoPrivateKey", err)
	}
}

func TestEd25519BackendVerifyRejectsNeverIssuedToken(t *testing.T) {
	pub, _, err := GenerateEd25519Keypair()
	if err != nil {
		t.Fatalf("GenerateEd25519Keypair: %v", err)
	}
	verifier := NewEd25519VerifierBackend(pub)
	ok, err := verifier.Verify(context.Background(), "never-issued")
	if err != nil || ok {
		t.Errorf("Verify(garbage, not even valid hex) = %v, %v, want false, nil", ok, err)
	}
}

func TestEd25519BackendVerifyRejectsExpiredToken(t *testing.T) {
	pub, priv, err := GenerateEd25519Keypair()
	if err != nil {
		t.Fatalf("GenerateEd25519Keypair: %v", err)
	}
	issuer := NewEd25519IssuerBackend(priv)
	issuer.TTL = time.Millisecond
	tok, err := issuer.Issue(context.Background())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	time.Sleep(5 * time.Millisecond)

	verifier := NewEd25519VerifierBackend(pub)
	ok, err := verifier.Verify(context.Background(), tok.Token)
	if err != nil || ok {
		t.Errorf("Verify(expired token) = %v, %v, want false, nil", ok, err)
	}
}

func TestEd25519BackendVerifyRejectsWrongKeypair(t *testing.T) {
	_, priv, err := GenerateEd25519Keypair()
	if err != nil {
		t.Fatalf("GenerateEd25519Keypair: %v", err)
	}
	issuer := NewEd25519IssuerBackend(priv)
	tok, err := issuer.Issue(context.Background())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	otherPub, _, err := GenerateEd25519Keypair()
	if err != nil {
		t.Fatalf("GenerateEd25519Keypair: %v", err)
	}
	verifier := NewEd25519VerifierBackend(otherPub)
	ok, err := verifier.Verify(context.Background(), tok.Token)
	if err != nil || ok {
		t.Errorf("Verify(token signed by a different keypair) = %v, %v, want false, nil", ok, err)
	}
}

func TestEd25519BackendVerifyRejectsTamperedToken(t *testing.T) {
	pub, priv, err := GenerateEd25519Keypair()
	if err != nil {
		t.Fatalf("GenerateEd25519Keypair: %v", err)
	}
	issuer := NewEd25519IssuerBackend(priv)
	tok, err := issuer.Issue(context.Background())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	raw, err := hex.DecodeString(tok.Token)
	if err != nil {
		t.Fatalf("decoding issued token: %v", err)
	}
	raw[0] ^= 0xff // flip a bit in the nonce
	tampered := hex.EncodeToString(raw)

	verifier := NewEd25519VerifierBackend(pub)
	ok, err := verifier.Verify(context.Background(), tampered)
	if err != nil || ok {
		t.Errorf("Verify(tampered token) = %v, %v, want false, nil", ok, err)
	}
}

func TestEd25519BackendVerifyRejectsWrongLength(t *testing.T) {
	pub, _, err := GenerateEd25519Keypair()
	if err != nil {
		t.Fatalf("GenerateEd25519Keypair: %v", err)
	}
	verifier := NewEd25519VerifierBackend(pub)
	for _, tok := range []string{"", "ab", hex.EncodeToString([]byte("too short to be real"))} {
		ok, err := verifier.Verify(context.Background(), tok)
		if err != nil || ok {
			t.Errorf("Verify(%q) = %v, %v, want false, nil", tok, ok, err)
		}
	}
}

func TestEd25519BackendIssueUsesDefaultTTLWhenZero(t *testing.T) {
	_, priv, err := GenerateEd25519Keypair()
	if err != nil {
		t.Fatalf("GenerateEd25519Keypair: %v", err)
	}
	issuer := NewEd25519IssuerBackend(priv)
	before := time.Now()
	tok, err := issuer.Issue(context.Background())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	want := before.Add(DefaultTTL)
	if tok.ExpiresAt.Before(want.Add(-time.Second)) || tok.ExpiresAt.After(want.Add(time.Second)) {
		t.Errorf("ExpiresAt = %v, want close to %v (DefaultTTL)", tok.ExpiresAt, want)
	}
}

func TestEd25519BackendIssueProducesDistinctTokens(t *testing.T) {
	_, priv, err := GenerateEd25519Keypair()
	if err != nil {
		t.Fatalf("GenerateEd25519Keypair: %v", err)
	}
	issuer := NewEd25519IssuerBackend(priv)
	tok1, err := issuer.Issue(context.Background())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	tok2, err := issuer.Issue(context.Background())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if tok1.Token == tok2.Token {
		t.Error("two Issue calls produced the identical token — the random nonce isn't doing its job")
	}
}

// TestEd25519BackendSatisfiesBackendAndIssuer is a compile-time-ish
// sanity check that this type is a drop-in alongside HMACBackend/
// StripeBackend wherever those interfaces are already accepted (e.g.
// MultiBackend, RequireToken, IssueHandler).
func TestEd25519BackendSatisfiesBackendAndIssuer(t *testing.T) {
	_, priv, err := GenerateEd25519Keypair()
	if err != nil {
		t.Fatalf("GenerateEd25519Keypair: %v", err)
	}
	var _ Backend = NewEd25519IssuerBackend(priv)
	var _ Issuer = NewEd25519IssuerBackend(priv)
}
