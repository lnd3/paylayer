package paylayer

import "context"

// MultiBackend is a Backend composed of several real backends, valid
// if ANY of them confirms a token — the mechanism EphemNet's own
// D012 needs once a second payment rail (Stripe, alongside the
// original Lightning-backed HMACBackend) exists: the two mint tokens
// with different signing keys and wire formats, so a single listener
// gated by RequireToken needs some way to accept either's output
// without caring which one actually produced it. Generically useful
// beyond that one case — any consumer adding a second payment method
// to an existing deployment hits the identical problem.
//
// Tries each backend in order, short-circuiting on the first
// confirmed-valid result. A hard error from one backend doesn't stop
// the rest from being tried — only if every backend fails outright
// (and none confirms valid) does Verify return that last error,
// matching the Backend interface's own "false and a non-nil error
// must be treated identically" contract either way.
type MultiBackend []Backend

// Verify reports whether token is valid against any backend in b.
func (b MultiBackend) Verify(ctx context.Context, token string) (bool, error) {
	var lastErr error
	for _, backend := range b {
		ok, err := backend.Verify(ctx, token)
		if err != nil {
			lastErr = err
			continue
		}
		if ok {
			return true, nil
		}
	}
	return false, lastErr
}
