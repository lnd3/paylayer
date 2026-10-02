package paylayer

import (
	"context"
	"errors"
	"testing"
)

func TestMultiBackendValidIfAnyBackendConfirms(t *testing.T) {
	hmacB := NewHMACBackend([]byte("hmac-key"))
	stripeB := NewStripeBackend([]byte("stripe-key"))
	multi := MultiBackend{hmacB, stripeB}

	hmacTok, err := hmacB.Issue(context.Background())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	ok, err := multi.Verify(context.Background(), hmacTok.Token)
	if err != nil || !ok {
		t.Errorf("Verify(hmac token) = %v, %v, want true, nil", ok, err)
	}

	stripeTok, err := stripeB.Issue(context.Background())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	ok, err = multi.Verify(context.Background(), stripeTok.Token)
	if err != nil || !ok {
		t.Errorf("Verify(stripe token) = %v, %v, want true, nil", ok, err)
	}
}

func TestMultiBackendRejectsTokenNoneConfirm(t *testing.T) {
	multi := MultiBackend{NewHMACBackend([]byte("k1")), NewStripeBackend([]byte("k2"))}
	ok, err := multi.Verify(context.Background(), "garbage")
	if err != nil || ok {
		t.Errorf("Verify(garbage) = %v, %v, want false, nil", ok, err)
	}
}

func TestMultiBackendOneBackendErroringDoesNotStopTryingOthers(t *testing.T) {
	hmacB := NewHMACBackend([]byte("hmac-key"))
	tok, err := hmacB.Issue(context.Background())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	failing := NewMock(1)
	failing.FailureMode, failing.FailureRate = FailureHardFail, 1.0
	multi := MultiBackend{failing, hmacB}
	ok, err := multi.Verify(context.Background(), tok.Token)
	if err != nil || !ok {
		t.Errorf("Verify(valid token, first backend hard-fails) = %v, %v, want true, nil", ok, err)
	}
}

func TestMultiBackendEmpty_AlwaysInvalid(t *testing.T) {
	var multi MultiBackend
	ok, err := multi.Verify(context.Background(), "anything")
	if err != nil || ok {
		t.Errorf("Verify on an empty MultiBackend = %v, %v, want false, nil", ok, err)
	}
}

// TestMultiBackendAllFail_ReturnsLastError covers the one case where
// an error does surface: every backend failed outright (none could
// even answer), not just "none confirmed valid" — a caller might
// reasonably want to distinguish total backend unavailability from an
// ordinary bad token, even though both are treated as "not authorized"
// for the request itself.
func TestMultiBackendAllFail_ReturnsLastError(t *testing.T) {
	failing := NewMock(1)
	failing.FailureMode, failing.FailureRate = FailureHardFail, 1.0
	multi := MultiBackend{failing}
	ok, err := multi.Verify(context.Background(), "anything")
	if ok || !errors.Is(err, ErrBackendUnavailable) {
		t.Errorf("Verify = %v, %v, want false, ErrBackendUnavailable", ok, err)
	}
}
