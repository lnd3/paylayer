package paylayer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPurchaseTokenExpired(t *testing.T) {
	now := time.Now()
	tok := PurchaseToken{Token: "x", ExpiresAt: now.Add(time.Minute)}
	if tok.Expired(now) {
		t.Errorf("token expiring a minute from now: Expired(now) = true, want false")
	}
	if !tok.Expired(now.Add(2 * time.Minute)) {
		t.Errorf("token that expired a minute ago: Expired = false, want true")
	}
	if !tok.Expired(tok.ExpiresAt) {
		t.Errorf("token evaluated exactly at its own ExpiresAt: Expired = false, want true (boundary is exclusive)")
	}
}

func TestRequireTokenRejectsMissingHeader(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	gated := RequireToken(inner, NewMock(1))

	req := httptest.NewRequest(http.MethodPost, "/v1/", nil)
	rec := httptest.NewRecorder()
	gated.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("no header: status = %d, want 404", rec.Code)
	}
}

func TestRequireTokenRejectsInvalidToken(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	gated := RequireToken(inner, NewMock(1)) // fresh mock, has never issued this token

	req := httptest.NewRequest(http.MethodPost, "/v1/", nil)
	req.Header.Set(TokenHeader, "never-issued")
	rec := httptest.NewRecorder()
	gated.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("invalid token: status = %d, want 404", rec.Code)
	}
}

func TestRequireTokenAllowsValidToken(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	backend := NewMock(1)
	tok, err := backend.Issue(context.Background())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	gated := RequireToken(inner, backend)

	req := httptest.NewRequest(http.MethodPost, "/v1/", nil)
	req.Header.Set(TokenHeader, tok.Token)
	rec := httptest.NewRecorder()
	gated.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !called {
		t.Errorf("valid token: status = %d called = %v, want 200 and the inner handler invoked", rec.Code, called)
	}
}

func TestRequireTokenRejectsBackendError(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	backend := NewMock(1)
	backend.FailureMode = FailureHardFail
	gated := RequireToken(inner, backend)

	req := httptest.NewRequest(http.MethodPost, "/v1/", nil)
	req.Header.Set(TokenHeader, "whatever")
	rec := httptest.NewRecorder()
	gated.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("backend error: status = %d, want 404 (a backend outage must not leak as anything other than \"not authorized\")", rec.Code)
	}
}
