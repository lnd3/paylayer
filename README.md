# paylayer

A payment middle layer whose contract is deliberately agnostic to how
payment was actually verified. A product only ever deals in one
concept:

```go
type PurchaseToken struct {
    Token     string
    ExpiresAt time.Time
}
```

A product's own write path checks exactly one thing — "is this token
present and not yet expired?" — and never sees macaroon, invoice,
Lightning, or any other payment-mechanism-specific detail. That
vocabulary is confined entirely to a real `Backend`'s own
implementation, one layer below this package.

## What's here

- `PurchaseToken`, the `Backend` interface, and `RequireToken` — an
  `http.Handler` wrapper that 404s unless a request carries a token a
  `Backend` confirms is currently valid.
- `StaticBackend` — a fixed, operator-configured value with no expiry.
  The simplest possible real backend; also what a consumer not ready
  to wire up genuine payment verification can start with.
- `Mock` — no real payment, for local development and tests.
  Deterministic (seeded) fault injection: configurable latency,
  timeouts (respects the caller's own `context.Context` deadline),
  hard failures, malformed responses, and premature expiry, plus a
  configurable failure rate for soak-style resilience testing. A mock
  that always succeeds instantly only ever proves the happy path.

## What's deliberately not here yet

A real payment-verified backend (Lightning/L402 via Aperture, or
anything else) — planned as its own package here once built, kept
physically separate from the contract so a second real backend
doesn't need to touch it.

### Planned: `HMACBackend`

Designed, not yet built (tracked in `EphemNet`'s own
`plan/designs/D012-real-lightning-backed-paylayer-backend.md`, which
owns the full decision history — this section only summarizes the
contract `paylayer` itself needs to expose).

- Mirrors `Mock`'s shape: `Issue(ctx) (PurchaseToken, error)` +
  `Verify(ctx, token) (bool, error)`.
- Backed by a single HMAC signing key, supplied by whoever constructs
  it (e.g. `NewHMACBackend(key []byte)`) — `paylayer` itself never
  generates, stores, or rotates this key; that's the embedding
  product's own operational concern (EphemNet's case: a flag/env-
  provided secret, generated once via `openssl rand -hex 32`,
  documented in that product's own deploy config, not here).
- `Issue` mints a self-verifying token (signed payload + embedded
  expiry); `Verify` checks the signature and expiry with **no network
  call and no shared state** — same cost profile as `StaticBackend`.
- Only becomes a key-*distribution* question if a consumer runs
  issuance and verification in separate processes — most consumers
  (EphemNet included) run both in one process sharing one instance,
  so the key never needs to leave process memory.
- A real Lightning/Aperture-backed issuer (invoice, macaroon,
  preimage) is a separate, product-side HTTP handler that calls
  `Issue` once payment is confirmed — that logic stays out of
  `paylayer` entirely, matching this package's existing
  payment-mechanism-agnostic contract.

Consumers other than EphemNet (`cinder`, `persona`) pick this up for
free on their next version bump once it's released — no action needed
until then.

## Origin

Extracted from [`cinder`](https://github.com/lnd3/cinder)'s
`internal/paylayer` once a second consumer (`EphemNet`) needed to
depend on it — Go's `internal/` visibility rule meant no other module
could import it from there regardless of how stable it was. Full
design history and rationale: `cinder`'s
`plan/designs/D012-purchase-token-contract.md` and
`plan/projects/P017-payment-middle-layer.md`.
