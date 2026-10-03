package paylayer

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"time"
)

func signHeader(secret []byte, timestamp int64, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(fmt.Sprintf("%d", timestamp)))
	mac.Write([]byte("."))
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("t=%d,v1=%s", timestamp, sig)
}

func TestVerifyStripeWebhookSignature_Valid(t *testing.T) {
	secret := []byte("whsec_test")
	body := []byte(`{"id":"evt_1"}`)
	now := time.Now()
	header := signHeader(secret, now.Unix(), body)

	if !VerifyStripeWebhookSignature(header, body, secret, now) {
		t.Error("expected a validly-signed, fresh webhook to verify")
	}
}

func TestVerifyStripeWebhookSignature_WrongSecret(t *testing.T) {
	body := []byte(`{"id":"evt_1"}`)
	now := time.Now()
	header := signHeader([]byte("wrong-secret"), now.Unix(), body)

	if VerifyStripeWebhookSignature(header, body, []byte("real-secret"), now) {
		t.Error("expected signature from the wrong secret to be rejected")
	}
}

func TestVerifyStripeWebhookSignature_TamperedBody(t *testing.T) {
	secret := []byte("whsec_test")
	now := time.Now()
	header := signHeader(secret, now.Unix(), []byte(`{"id":"evt_1"}`))

	if VerifyStripeWebhookSignature(header, []byte(`{"id":"evt_2"}`), secret, now) {
		t.Error("expected a tampered body to be rejected even with a valid-looking header")
	}
}

func TestVerifyStripeWebhookSignature_TooOld(t *testing.T) {
	secret := []byte("whsec_test")
	body := []byte(`{"id":"evt_1"}`)
	old := time.Now().Add(-10 * time.Minute)
	header := signHeader(secret, old.Unix(), body)

	if VerifyStripeWebhookSignature(header, body, secret, time.Now()) {
		t.Error("expected a webhook older than the tolerance window to be rejected (replay protection)")
	}
}

func TestVerifyStripeWebhookSignature_TooFarInFuture(t *testing.T) {
	secret := []byte("whsec_test")
	body := []byte(`{"id":"evt_1"}`)
	future := time.Now().Add(10 * time.Minute)
	header := signHeader(secret, future.Unix(), body)

	if VerifyStripeWebhookSignature(header, body, secret, time.Now()) {
		t.Error("expected a webhook timestamped far in the future to be rejected too, not just old ones")
	}
}

func TestVerifyStripeWebhookSignature_MalformedHeader(t *testing.T) {
	secret := []byte("whsec_test")
	body := []byte(`{"id":"evt_1"}`)
	for _, header := range []string{"", "garbage", "t=123", "v1=abc", "t=notanumber,v1=abc"} {
		if VerifyStripeWebhookSignature(header, body, secret, time.Now()) {
			t.Errorf("VerifyStripeWebhookSignature(%q) = true, want false", header)
		}
	}
}

// TestVerifyStripeSignature_MultipleV1_AnyMatching covers a real
// Stripe behavior: during a webhook signing-secret rotation, a
// delivery carries both the old and new secret's signature — either
// one matching must be accepted.
func TestVerifyStripeWebhookSignature_MultipleV1_AnyMatching(t *testing.T) {
	newSecret := []byte("new-secret")
	body := []byte(`{"id":"evt_1"}`)
	now := time.Now()

	mac := hmac.New(sha256.New, []byte("old-secret"))
	mac.Write([]byte(fmt.Sprintf("%d", now.Unix())))
	mac.Write([]byte("."))
	mac.Write(body)
	oldSig := hex.EncodeToString(mac.Sum(nil))

	mac2 := hmac.New(sha256.New, newSecret)
	mac2.Write([]byte(fmt.Sprintf("%d", now.Unix())))
	mac2.Write([]byte("."))
	mac2.Write(body)
	newSig := hex.EncodeToString(mac2.Sum(nil))

	header := fmt.Sprintf("t=%d,v1=%s,v1=%s", now.Unix(), oldSig, newSig)
	if !VerifyStripeWebhookSignature(header, body, newSecret, now) {
		t.Error("expected verification against the new secret to succeed when its matching v1 is present alongside an old one")
	}
}
