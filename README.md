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

- `HMACBackend` — a self-verifying `Backend`: `Issue` mints a token
  whose own bytes carry a random nonce, an embedded expiry, and an
  HMAC-SHA256 signature over both; `Verify` checks the signature and
  expiry with **no network call and no shared state** — same cost
  profile as `StaticBackend`, while still proving whoever presents a
  token is holding something only `Issue`'s own caller could have
  produced (the same signing key, `NewHMACBackend(key []byte)`).
  `paylayer` itself never generates, stores, or rotates that key —
  that's the embedding product's own operational concern (EphemNet's
  case: a flag/env-provided secret, generated once via
  `openssl rand -hex 32`). Only becomes a key-*distribution* question
  if a consumer runs issuance and verification in separate processes
  — most consumers (EphemNet included) run both in one process
  sharing one instance. Full decision history: `EphemNet`'s own
  `plan/designs/D012-real-lightning-backed-paylayer-backend.md`.

- `StripeBackend` — a self-verifying `Backend`+`Issuer` for
  Stripe-gated purchases, mechanically near-identical to `HMACBackend`
  (same nonce+expiry+HMAC-SHA256 wire shape, no network call, no
  shared state), with one real addition: every token also carries a
  signed, tamper-evident **`Quantity`** (`IssueTopUp(ctx, quantity)` +
  `Quantity(token) (int, bool)`), so a caller can recover not just
  "was this purchase valid" but "how many units were purchased" — for
  a metered product sold in predefined top-up packages (e.g.
  EphemNet's own `ephemnet-relay`, by the GB), where a flat yes/no
  isn't enough information to credit the right amount to an internal
  usage ledger. Plain `Issue` (satisfying `Issuer`) mints a flat
  `Quantity` of 1, for a non-metered purchase (e.g. `ephemnet-domain`'s
  own annual subscription) with nothing to count. Contains zero
  Stripe-specific vocabulary of its own (no webhook signature
  verification, no price IDs, no Checkout Session parsing, no Stripe
  SDK dependency) — same reasoning `HMACBackend` has no Lightning
  vocabulary; a product's own Stripe webhook handler decides when to
  call `Issue`/`IssueTopUp` and with what `Quantity`, entirely outside
  this package. Interim/parallel to `HMACBackend`, not a replacement —
  either can issue a token for the same product, and a listener gated
  by `RequireToken` can't tell which payment method produced the token
  it's holding, by design. Full decision history: `EphemNet`'s own
  `plan/designs/D012-real-lightning-backed-paylayer-backend.md`.

- `Issuer` and `IssueHandler(issuer Issuer)` — the generic "mint a
  token and hand it back as JSON" glue any issuer-fronting service
  needs, regardless of what actually gates the call. `Issuer` is
  deliberately separate from `Backend` (only `Mock`/`HMACBackend`/
  `StripeBackend` implement it; `StaticBackend` never issues anything).
  `IssueHandler`
  has no payment vocabulary of its own — whatever sits in front of it
  (a self-hosted Aperture instance gating on a real Lightning
  payment, a test harness, anything else) decides whether a given
  request is even allowed to reach it. POST-only (issuing a token is
  a real side effect, not an idempotent read).

- `MultiBackend` — a `Backend` composed of several real backends,
  valid if ANY confirms a token. Exists for exactly the situation a
  second payment rail creates (EphemNet's own case: `HMACBackend`
  for Lightning, `StripeBackend` for Stripe, different keys and wire
  formats, one listener needing to accept either's output) — tries
  each in order, short-circuits on the first confirmed-valid result,
  and only surfaces an error if every backend failed outright (none
  could even answer) rather than merely "didn't confirm this one."

## What's deliberately not here yet

The actual Lightning/L402 gating itself (invoice, macaroon, preimage)
— that's entirely Aperture's own job, configured to front
`IssueHandler`, fronted by whatever reverse proxy a consumer already
uses (EphemNet's case: the Aperture-facing listener and
`-paid-registration-addr` wiring, both genuinely product-specific, so
built directly in EphemNet, not here). `paylayer` never gains
Lightning/Aperture vocabulary of its own.

Consumers other than EphemNet (`cinder`, `persona`) pick up
`HMACBackend`/`IssueHandler` for free on their next version bump — no
action needed until then.

## Origin

Extracted from [`cinder`](https://github.com/lnd3/cinder)'s
`internal/paylayer` once a second consumer (`EphemNet`) needed to
depend on it — Go's `internal/` visibility rule meant no other module
could import it from there regardless of how stable it was. Full
design history and rationale: `cinder`'s
`plan/designs/D012-purchase-token-contract.md` and
`plan/projects/P017-payment-middle-layer.md`.
