package paylayer

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// stripeCheckoutDefaultBaseURL is Stripe's own real API —
// overridable (see StripeCheckoutClient.BaseURL) so a test can point
// this at an httptest.Server instead of ever making a real network
// call.
const stripeCheckoutDefaultBaseURL = "https://api.stripe.com"

// StripeCheckoutClient creates Checkout Sessions via Stripe's own
// API. Extracted here 2026-10-03 (originally built inside EphemNet's
// own internal/stripecheckout) once a second consumer needed the
// identical, genuinely Stripe-generic mechanics — this type has no
// product vocabulary of its own at all (callers supply their own
// Price IDs and metadata), the same reason StripeBackend itself lives
// here rather than being duplicated per repo. Hand-rolls the one
// Stripe API call actually needed (POST /v1/checkout/sessions, plain
// form-encoded REST, HTTP Basic Auth with the secret key as username)
// rather than depending on the full stripe-go SDK. Construct with
// NewStripeCheckoutClient.
type StripeCheckoutClient struct {
	apiKey string

	// BaseURL overrides stripeCheckoutDefaultBaseURL — tests only,
	// real use leaves this unset.
	BaseURL string

	HTTPClient *http.Client
}

// NewStripeCheckoutClient returns a StripeCheckoutClient
// authenticating with apiKey — a Stripe secret key (starts "sk_"),
// NOT the same value as a webhook secret (verified separately, e.g.
// via VerifyStripeWebhookSignature) or StripeBackend's own internal
// signing key: three distinct secrets, three distinct jobs (this one
// proves to STRIPE who's calling; a webhook secret proves to the
// receiver that Stripe is who's calling; StripeBackend's own key
// makes a PurchaseToken self-verifying once minted — none of them can
// substitute for another).
func NewStripeCheckoutClient(apiKey string) *StripeCheckoutClient {
	return &StripeCheckoutClient{apiKey: apiKey, HTTPClient: &http.Client{}}
}

// StripeLineItem is one Price (and how many of it) a Checkout Session
// sells. A consumer selling predefined packages (different sizes/
// tiers as different Prices, D012's own pattern) uses Quantity 1 for
// each — a different package size is a different Price entirely, not
// the same Price bought in bulk.
type StripeLineItem struct {
	PriceID  string
	Quantity int
}

// StripeCheckoutSessionParams is everything CreateSession needs to
// build one Checkout Session. Metadata is the whole reason this type
// exists instead of just handing someone a raw Stripe dashboard
// Payment Link: it's stamped onto the session now, and comes back
// verbatim on the resulting checkout.session.completed event, without
// whatever receives that webhook ever needing to call back into
// Stripe's own API to look anything up.
type StripeCheckoutSessionParams struct {
	LineItems  []StripeLineItem
	SuccessURL string
	CancelURL  string
	Metadata   map[string]string

	// AutomaticTax turns on Stripe Tax for this session — a real
	// product/legal decision (VAT/sales-tax compliance), not an
	// engineering default. Left to the caller, never hardcoded here.
	AutomaticTax bool
}

// StripeCheckoutSession is the subset of Stripe's own Checkout
// Session object CreateSession actually returns — just enough to
// redirect a customer to Stripe-hosted checkout.
type StripeCheckoutSession struct {
	ID  string
	URL string
}

// CreateSession creates a real Checkout Session (mode: payment —
// always a one-time purchase, never mode: subscription; matches this
// package's own no-subscriptions-or-stored-balances discipline, see
// HMACBackend's and StripeBackend's own doc comments) and returns its
// id and hosted checkout URL.
func (c *StripeCheckoutClient) CreateSession(params StripeCheckoutSessionParams) (StripeCheckoutSession, error) {
	if len(params.LineItems) == 0 {
		return StripeCheckoutSession{}, fmt.Errorf("paylayer: at least one line item is required")
	}
	if params.SuccessURL == "" || params.CancelURL == "" {
		return StripeCheckoutSession{}, fmt.Errorf("paylayer: SuccessURL and CancelURL are both required")
	}

	form := url.Values{}
	form.Set("mode", "payment")
	form.Set("success_url", params.SuccessURL)
	form.Set("cancel_url", params.CancelURL)
	if params.AutomaticTax {
		form.Set("automatic_tax[enabled]", "true")
	}
	for i, li := range params.LineItems {
		if li.PriceID == "" {
			return StripeCheckoutSession{}, fmt.Errorf("paylayer: line item %d has no PriceID", i)
		}
		qty := li.Quantity
		if qty <= 0 {
			qty = 1
		}
		form.Set(fmt.Sprintf("line_items[%d][price]", i), li.PriceID)
		form.Set(fmt.Sprintf("line_items[%d][quantity]", i), fmt.Sprintf("%d", qty))
	}
	for k, v := range params.Metadata {
		form.Set(fmt.Sprintf("metadata[%s]", k), v)
	}

	baseURL := c.BaseURL
	if baseURL == "" {
		baseURL = stripeCheckoutDefaultBaseURL
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+"/v1/checkout/sessions", strings.NewReader(form.Encode()))
	if err != nil {
		return StripeCheckoutSession{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(c.apiKey, "")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return StripeCheckoutSession{}, fmt.Errorf("calling Stripe: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return StripeCheckoutSession{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return StripeCheckoutSession{}, fmt.Errorf("stripe API %s: %s", resp.Status, body)
	}

	var raw struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return StripeCheckoutSession{}, fmt.Errorf("decoding Stripe response: %w", err)
	}
	return StripeCheckoutSession{ID: raw.ID, URL: raw.URL}, nil
}
