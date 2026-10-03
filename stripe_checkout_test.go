package paylayer

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestStripeCheckoutCreateSession_SendsExpectedRequestAndParsesResponse(t *testing.T) {
	var gotMethod, gotPath, gotAuthUser, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		user, _, _ := r.BasicAuth()
		gotAuthUser = user
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"cs_test_123","url":"https://checkout.stripe.com/pay/cs_test_123"}`)
	}))
	defer server.Close()

	c := NewStripeCheckoutClient("sk_test_secret")
	c.BaseURL = server.URL
	session, err := c.CreateSession(StripeCheckoutSessionParams{
		LineItems:  []StripeLineItem{{PriceID: "price_abc", Quantity: 1}},
		SuccessURL: "https://example.com/success",
		CancelURL:  "https://example.com/cancel",
		Metadata:   map[string]string{"ephemnet_product": "domain-register", "domain": "foo.example"},
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/v1/checkout/sessions" {
		t.Errorf("path = %q, want /v1/checkout/sessions", gotPath)
	}
	if gotAuthUser != "sk_test_secret" {
		t.Errorf("basic auth user = %q, want the API key", gotAuthUser)
	}
	for _, want := range []string{"mode=payment", "line_items%5B0%5D%5Bprice%5D=price_abc", "metadata%5Bdomain%5D=foo.example"} {
		if !strings.Contains(gotBody, want) {
			t.Errorf("request body missing %q: %s", want, gotBody)
		}
	}
	if session.ID != "cs_test_123" || session.URL != "https://checkout.stripe.com/pay/cs_test_123" {
		t.Errorf("unexpected session: %+v", session)
	}
}

func TestStripeCheckoutCreateSession_AutomaticTax(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"cs_1","url":"https://checkout.stripe.com/pay/cs_1"}`)
	}))
	defer server.Close()

	c := NewStripeCheckoutClient("sk_test_secret")
	c.BaseURL = server.URL
	_, err := c.CreateSession(StripeCheckoutSessionParams{
		LineItems:    []StripeLineItem{{PriceID: "price_abc"}},
		SuccessURL:   "https://example.com/success",
		CancelURL:    "https://example.com/cancel",
		AutomaticTax: true,
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if !strings.Contains(gotBody, "automatic_tax%5Benabled%5D=true") {
		t.Errorf("request body missing automatic_tax[enabled]=true: %s", gotBody)
	}
}

func TestStripeCheckoutCreateSession_ZeroQuantityDefaultsToOne(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		fmt.Fprint(w, `{"id":"cs_1","url":"https://checkout.stripe.com/pay/cs_1"}`)
	}))
	defer server.Close()

	c := NewStripeCheckoutClient("sk_test_secret")
	c.BaseURL = server.URL
	_, err := c.CreateSession(StripeCheckoutSessionParams{
		LineItems:  []StripeLineItem{{PriceID: "price_abc", Quantity: 0}},
		SuccessURL: "https://example.com/success",
		CancelURL:  "https://example.com/cancel",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if !strings.Contains(gotBody, "line_items%5B0%5D%5Bquantity%5D=1") {
		t.Errorf("expected quantity to default to 1: %s", gotBody)
	}
}

func TestStripeCheckoutCreateSession_RequiresAtLeastOneLineItem(t *testing.T) {
	c := NewStripeCheckoutClient("sk_test_secret")
	_, err := c.CreateSession(StripeCheckoutSessionParams{SuccessURL: "https://x", CancelURL: "https://y"})
	if err == nil {
		t.Fatal("expected an error with zero line items")
	}
}

func TestStripeCheckoutCreateSession_RequiresSuccessAndCancelURL(t *testing.T) {
	c := NewStripeCheckoutClient("sk_test_secret")
	li := []StripeLineItem{{PriceID: "price_abc"}}
	if _, err := c.CreateSession(StripeCheckoutSessionParams{LineItems: li, CancelURL: "https://y"}); err == nil {
		t.Fatal("expected an error with no SuccessURL")
	}
	if _, err := c.CreateSession(StripeCheckoutSessionParams{LineItems: li, SuccessURL: "https://x"}); err == nil {
		t.Fatal("expected an error with no CancelURL")
	}
}

func TestStripeCheckoutCreateSession_LineItemMissingPriceID(t *testing.T) {
	c := NewStripeCheckoutClient("sk_test_secret")
	_, err := c.CreateSession(StripeCheckoutSessionParams{
		LineItems:  []StripeLineItem{{Quantity: 1}},
		SuccessURL: "https://x", CancelURL: "https://y",
	})
	if err == nil {
		t.Fatal("expected an error for a line item with no PriceID")
	}
}

func TestStripeCheckoutCreateSession_StripeErrorSurfaced(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":{"message":"No such price: 'price_bad'"}}`)
	}))
	defer server.Close()

	c := NewStripeCheckoutClient("sk_test_secret")
	c.BaseURL = server.URL
	_, err := c.CreateSession(StripeCheckoutSessionParams{
		LineItems:  []StripeLineItem{{PriceID: "price_bad"}},
		SuccessURL: "https://x", CancelURL: "https://y",
	})
	if err == nil {
		t.Fatal("expected Stripe's own error response to surface as an error")
	}
	if !strings.Contains(err.Error(), "price_bad") {
		t.Errorf("error doesn't mention the real Stripe error detail: %v", err)
	}
}

func TestGetPrice_SendsExpectedRequestAndParsesResponse(t *testing.T) {
	var gotMethod, gotPath, gotAuthUser string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		user, _, _ := r.BasicAuth()
		gotAuthUser = user
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"price_abc","unit_amount":499,"currency":"usd"}`)
	}))
	defer server.Close()

	c := NewStripeCheckoutClient("sk_test_secret")
	c.BaseURL = server.URL
	price, err := c.GetPrice("price_abc")
	if err != nil {
		t.Fatalf("GetPrice: %v", err)
	}

	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if gotPath != "/v1/prices/price_abc" {
		t.Errorf("path = %q, want /v1/prices/price_abc", gotPath)
	}
	if gotAuthUser != "sk_test_secret" {
		t.Errorf("basic auth user = %q, want the API key", gotAuthUser)
	}
	if price.ID != "price_abc" || price.UnitAmount != 499 || price.Currency != "usd" {
		t.Errorf("unexpected price: %+v", price)
	}
}

func TestGetPrice_StripeErrorSurfaced(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":{"message":"No such price: 'price_missing'"}}`)
	}))
	defer server.Close()

	c := NewStripeCheckoutClient("sk_test_secret")
	c.BaseURL = server.URL
	_, err := c.GetPrice("price_missing")
	if err == nil {
		t.Fatal("expected Stripe's own error response to surface as an error")
	}
	if !strings.Contains(err.Error(), "price_missing") {
		t.Errorf("error doesn't mention the real Stripe error detail: %v", err)
	}
}

func TestRefund_SendsExpectedRequest(t *testing.T) {
	var gotMethod, gotPath, gotAuthUser, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		user, _, _ := r.BasicAuth()
		gotAuthUser = user
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"re_abc","status":"succeeded"}`)
	}))
	defer server.Close()

	c := NewStripeCheckoutClient("sk_test_secret")
	c.BaseURL = server.URL
	if err := c.Refund("pi_abc"); err != nil {
		t.Fatalf("Refund: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/v1/refunds" {
		t.Errorf("path = %q, want /v1/refunds", gotPath)
	}
	if gotAuthUser != "sk_test_secret" {
		t.Errorf("basic auth user = %q, want the API key", gotAuthUser)
	}
	values, err := url.ParseQuery(gotBody)
	if err != nil {
		t.Fatalf("parsing request body: %v", err)
	}
	if got := values.Get("payment_intent"); got != "pi_abc" {
		t.Errorf("payment_intent = %q, want pi_abc", got)
	}
}

func TestRefund_MissingPaymentIntentID_Rejected(t *testing.T) {
	c := NewStripeCheckoutClient("sk_test_secret")
	if err := c.Refund(""); err == nil {
		t.Fatal("expected an error for an empty paymentIntentID")
	}
}

func TestRefund_StripeErrorSurfaced(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":{"message":"Charge pi_missing has already been refunded"}}`)
	}))
	defer server.Close()

	c := NewStripeCheckoutClient("sk_test_secret")
	c.BaseURL = server.URL
	err := c.Refund("pi_missing")
	if err == nil {
		t.Fatal("expected Stripe's own error response to surface as an error")
	}
	if !strings.Contains(err.Error(), "pi_missing") {
		t.Errorf("error doesn't mention the real Stripe error detail: %v", err)
	}
}
