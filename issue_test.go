package paylayer

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIssueHandlerReturnsIssuedToken(t *testing.T) {
	h := IssueHandler(NewHMACBackend([]byte("test-key")))

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	var got issueResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshaling response: %v", err)
	}
	if got.Token == "" {
		t.Fatal("response has no token")
	}
	if got.ExpiresAt.IsZero() {
		t.Fatal("response has no expires_at")
	}
}

// TestIssueHandlerIssuedTokenActuallyVerifies covers the real
// end-to-end property: a token this handler hands out must be
// usable, not just well-formed — checked here against the same
// HMACBackend instance (the real deployment shape: one Backend shared
// by both the issuer handler and the verifying listener).
func TestIssueHandlerIssuedTokenActuallyVerifies(t *testing.T) {
	backend := NewHMACBackend([]byte("test-key"))
	h := IssueHandler(backend)

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var got issueResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshaling response: %v", err)
	}
	ok, err := backend.Verify(context.Background(), got.Token)
	if err != nil || !ok {
		t.Errorf("Verify(issued token) = %v, %v, want true, nil", ok, err)
	}
}

func TestIssueHandlerRejectsNonPost(t *testing.T) {
	h := IssueHandler(NewHMACBackend([]byte("test-key")))
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		req := httptest.NewRequest(method, "/", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: status = %d, want 405", method, rec.Code)
		}
	}
}

type failingIssuer struct{}

func (failingIssuer) Issue(ctx context.Context) (PurchaseToken, error) {
	return PurchaseToken{}, errors.New("simulated issuer failure")
}

func TestIssueHandlerReturns500OnIssuerError(t *testing.T) {
	h := IssueHandler(failingIssuer{})
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

// TestIssueHandlerWorksWithMockToo covers that this handler is
// genuinely generic — not accidentally coupled to HMACBackend's own
// concrete type, despite every other test here using it.
func TestIssueHandlerWorksWithMockToo(t *testing.T) {
	h := IssueHandler(NewMock(1))
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
}
