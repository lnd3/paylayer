package paylayer

import (
	"context"
	"testing"
)

func TestStaticBackendAcceptsExactMatch(t *testing.T) {
	b := NewStaticBackend("correct-secret")
	ok, err := b.Verify(context.Background(), "correct-secret")
	if err != nil || !ok {
		t.Errorf("Verify(exact match) = %v, %v, want true, nil", ok, err)
	}
}

func TestStaticBackendRejectsWrongValue(t *testing.T) {
	b := NewStaticBackend("correct-secret")
	ok, err := b.Verify(context.Background(), "wrong-secret-same-ish-length")
	if err != nil || ok {
		t.Errorf("Verify(wrong value) = %v, %v, want false, nil", ok, err)
	}
}

func TestStaticBackendRejectsDifferentLength(t *testing.T) {
	b := NewStaticBackend("correct-secret")
	ok, err := b.Verify(context.Background(), "short")
	if err != nil || ok {
		t.Errorf("Verify(different length) = %v, %v, want false, nil", ok, err)
	}
}

func TestStaticBackendRejectsEmptyToken(t *testing.T) {
	b := NewStaticBackend("correct-secret")
	ok, err := b.Verify(context.Background(), "")
	if err != nil || ok {
		t.Errorf("Verify(empty token) = %v, %v, want false, nil", ok, err)
	}
}
