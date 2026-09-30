package paylayer

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMockIssueThenVerifyRoundTrips(t *testing.T) {
	m := NewMock(1)
	tok, err := m.Issue(context.Background())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	ok, err := m.Verify(context.Background(), tok.Token)
	if err != nil || !ok {
		t.Errorf("Verify(issued token) = %v, %v, want true, nil", ok, err)
	}
}

func TestMockVerifyRejectsUnknownToken(t *testing.T) {
	m := NewMock(1)
	ok, err := m.Verify(context.Background(), "never-issued")
	if err != nil || ok {
		t.Errorf("Verify(unknown token) = %v, %v, want false, nil", ok, err)
	}
}

func TestMockVerifyRejectsExpiredToken(t *testing.T) {
	m := NewMock(1)
	m.TTL = time.Millisecond
	tok, err := m.Issue(context.Background())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	ok, err := m.Verify(context.Background(), tok.Token)
	if err != nil || ok {
		t.Errorf("Verify(expired token) = %v, %v, want false, nil", ok, err)
	}
}

func TestMockFailurePrematureExpiryIssuesAlreadyExpiredToken(t *testing.T) {
	m := NewMock(1)
	m.FailureMode = FailurePrematureExpiry
	tok, err := m.Issue(context.Background())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if !tok.Expired(time.Now()) {
		t.Errorf("token issued under FailurePrematureExpiry: Expired(now) = false, want true")
	}
	// And Verify must independently reject it too — the fault is real
	// expiry, not just a misleading ExpiresAt field.
	ok, err := m.Verify(context.Background(), tok.Token)
	if err != nil || ok {
		t.Errorf("Verify(prematurely-expired token) = %v, %v, want false, nil", ok, err)
	}
}

func TestMockFailureHardFailReturnsErrOnIssueAndVerify(t *testing.T) {
	m := NewMock(1)
	m.FailureMode = FailureHardFail

	if _, err := m.Issue(context.Background()); !errors.Is(err, ErrBackendUnavailable) {
		t.Errorf("Issue under FailureHardFail: err = %v, want ErrBackendUnavailable", err)
	}
	if _, err := m.Verify(context.Background(), "anything"); !errors.Is(err, ErrBackendUnavailable) {
		t.Errorf("Verify under FailureHardFail: err = %v, want ErrBackendUnavailable", err)
	}
}

func TestMockFailureMalformedIssuedTokenNeverVerifies(t *testing.T) {
	m := NewMock(1)
	m.FailureMode = FailureMalformed
	tok, err := m.Issue(context.Background())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	// Verify itself is also under FailureMalformed here, which always
	// reports true regardless — so disable it to actually observe that
	// the token Issue handed back was never recorded for real.
	m.FailureMode = FailureNone
	ok, err := m.Verify(context.Background(), tok.Token)
	if err != nil || ok {
		t.Errorf("Verify(token from a malformed Issue, fault mode now cleared) = %v, %v, want false, nil — the token was never actually recorded", ok, err)
	}
}

func TestMockFailureMalformedVerifyAlwaysReportsValid(t *testing.T) {
	m := NewMock(1)
	m.FailureMode = FailureMalformed
	ok, err := m.Verify(context.Background(), "some-token-never-issued")
	if err != nil || !ok {
		t.Errorf("Verify under FailureMalformed = %v, %v, want true, nil (simulates a backend falsely confirming an unknown token)", ok, err)
	}
}

func TestMockFailureTimeoutRespectsCallerContextDeadline(t *testing.T) {
	m := NewMock(1)
	m.FailureMode = FailureTimeout

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := m.Verify(ctx, "anything")
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Verify under FailureTimeout: err = %v, want context.DeadlineExceeded", err)
	}
	if elapsed > time.Second {
		t.Errorf("Verify under FailureTimeout took %v to respect a 20ms caller deadline — the caller's own deadline should win the race, not the mock's internal delay", elapsed)
	}
}

func TestMockLatencyIsActuallyWaited(t *testing.T) {
	m := NewMock(1)
	m.Latency = 30 * time.Millisecond

	start := time.Now()
	if _, err := m.Issue(context.Background()); err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if elapsed := time.Since(start); elapsed < m.Latency {
		t.Errorf("Issue with Latency=%v returned after only %v, want at least that long", m.Latency, elapsed)
	}
}

// TestMockFailureRateIsDeterministicForAFixedSeed confirms fault
// injection is reproducible, not flaky: two Mocks built from the same
// seed, given the same sequence of calls, must make the identical
// sequence of fault/no-fault decisions.
func TestMockFailureRateIsDeterministicForAFixedSeed(t *testing.T) {
	const seed = 42
	const n = 50

	run := func() []bool {
		m := NewMock(seed)
		m.FailureMode = FailureHardFail
		m.FailureRate = 0.5
		results := make([]bool, n)
		for i := 0; i < n; i++ {
			_, err := m.Verify(context.Background(), "x")
			results[i] = err == nil
		}
		return results
	}

	first := run()
	second := run()
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("call %d: first run succeeded=%v, second run succeeded=%v — same seed must reproduce the identical fault sequence", i, first[i], second[i])
		}
	}

	// Sanity: a 0.5 rate over 50 calls should produce some of each,
	// not degenerate to all-pass or all-fail (which would indicate a
	// broken rate check, not just bad luck, given the fixed seed).
	successes := 0
	for _, ok := range first {
		if ok {
			successes++
		}
	}
	if successes == 0 || successes == n {
		t.Errorf("FailureRate=0.5 over %d calls: successes = %d, want a genuine mix", n, successes)
	}
}

// TestMockFailureRateZeroMeansAlways confirms the documented default:
// setting FailureMode without setting FailureRate always applies the
// fault, rather than a Go zero-value rate silently meaning "never."
func TestMockFailureRateZeroMeansAlways(t *testing.T) {
	m := NewMock(1)
	m.FailureMode = FailureHardFail
	for i := 0; i < 20; i++ {
		if _, err := m.Verify(context.Background(), "x"); !errors.Is(err, ErrBackendUnavailable) {
			t.Fatalf("call %d: err = %v, want ErrBackendUnavailable every time when FailureRate is left at its zero value", i, err)
		}
	}
}
