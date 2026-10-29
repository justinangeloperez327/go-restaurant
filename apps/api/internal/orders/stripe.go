package orders

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type StripeClient struct {
	SecretKey     string
	WebhookSecret string
	HTTP          *http.Client
	APIURL        string // Override only in tests; production uses api.stripe.com.
}

func (s StripeClient) CreateCheckout(ctx context.Context, in StripeCheckoutRequest) (StripeCheckoutSession, error) {
	var out StripeCheckoutSession
	if s.SecretKey == "" {
		return out, errors.New("Stripe secret key not configured")
	}
	endpoint := s.APIURL
	if endpoint == "" {
		endpoint = "https://api.stripe.com/v1/checkout/sessions"
	}
	form := url.Values{}
	form.Set("mode", "payment")
	form.Set("payment_method_types[0]", "card")
	form.Set("client_reference_id", in.OrderID)
	form.Set("metadata[order_id]", in.OrderID)
	form.Set("payment_intent_data[metadata][order_id]", in.OrderID)
	form.Set("success_url", in.SuccessURL)
	form.Set("cancel_url", in.CancelURL)
	form.Set("line_items[0][price_data][currency]", in.Currency)
	form.Set("line_items[0][price_data][unit_amount]", strconv.FormatInt(in.AmountMinor, 10))
	form.Set("line_items[0][price_data][product_data][name]", "Restaurant order "+in.OrderID[:8])
	form.Set("line_items[0][quantity]", "1")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return out, err
	}
	req.SetBasicAuth(s.SecretKey, "")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Idempotency-Key", in.IdempotencyKey)
	client := s.HTTP
	if client == nil {
		client = &http.Client{Timeout: 12 * time.Second}
	}
	res, err := client.Do(req)
	if err != nil {
		return out, err
	}
	defer res.Body.Close()
	limited := io.LimitReader(res.Body, 64*1024)
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		b, _ := io.ReadAll(limited)
		return out, fmt.Errorf("Stripe session API status %d (%d bytes)", res.StatusCode, len(b))
	}
	if err := json.NewDecoder(limited).Decode(&out); err != nil {
		return out, err
	}
	if out.ID == "" || !strings.HasPrefix(out.URL, "https://checkout.stripe.com/") {
		return out, errors.New("invalid Stripe Checkout response")
	}
	return out, nil
}

func verifyStripePayload(body []byte, header, secret string, now time.Time) bool {
	if secret == "" || len(body) == 0 || len(header) > 2048 {
		return false
	}
	var timestamp string
	var digests []string
	for _, part := range strings.Split(header, ",") {
		parts := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(parts) != 2 {
			continue
		}
		if parts[0] == "t" {
			timestamp = parts[1]
		}
		if parts[0] == "v1" {
			digests = append(digests, parts[1])
		}
	}
	sec, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || len(digests) == 0 {
		return false
	}
	dt := now.Unix() - sec
	if dt > 300 || dt < -300 {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	expected := mac.Sum(nil)
	for _, signature := range digests {
		parsed, err := hex.DecodeString(signature)
		if err == nil && hmac.Equal(parsed, expected) {
			return true
		}
	}
	return false
}

type stripeEvent struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Data struct {
		Object struct {
			ID                string            `json:"id"`
			ClientReferenceID string            `json:"client_reference_id"`
			AmountTotal       int64             `json:"amount_total"`
			Currency          string            `json:"currency"`
			PaymentStatus     string            `json:"payment_status"`
			PaymentIntent     string            `json:"payment_intent"`
			Metadata          map[string]string `json:"metadata"`
		} `json:"object"`
	} `json:"data"`
}

func (s Service) handleStripeWebhook(w http.ResponseWriter, r *http.Request) {
	gateway, ok := s.Stripe.(StripeClient)
	if !ok || gateway.WebhookSecret == "" {
		fail(w, 503, "webhook is not configured")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 128*1024)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		fail(w, 400, "invalid event")
		return
	}
	if !verifyStripePayload(raw, r.Header.Get("Stripe-Signature"), gateway.WebhookSecret, time.Now()) {
		fail(w, 400, "invalid payment signature")
		return
	}
	var event stripeEvent
	if err := json.NewDecoder(bytes.NewReader(raw)).Decode(&event); err != nil || event.ID == "" {
		fail(w, 400, "invalid event")
		return
	}
	switch event.Type {
	case "checkout.session.completed", "checkout.session.async_payment_succeeded", "checkout.session.async_payment_failed", "checkout.session.expired":
	default:
		w.WriteHeader(204)
		return
	}
	session := event.Data.Object
	orderID := session.Metadata["order_id"]
	if !uuidRE.MatchString(orderID) || session.ID == "" || session.ClientReferenceID != orderID {
		fail(w, 400, "unrecognized payment session")
		return
	}
	if session.PaymentStatus != "paid" && event.Type == "checkout.session.async_payment_succeeded" {
		fail(w, 400, "success event without paid state")
		return
	}
	if event.Type == "checkout.session.completed" && session.PaymentStatus != "paid" {
		// Asynchronous methods do not cause fulfillment until payment succeeded.
		w.WriteHeader(204)
		return
	}
	ctx := r.Context()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		fail(w, 503, "payment database unavailable")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var knownID, method, paymentStatus, currency, storedSession string
	var total int64
	err = tx.QueryRow(ctx, `SELECT id::text,payment_method,payment_status,currency,total_minor,COALESCE(stripe_session_id,'')
 FROM orders WHERE id=$1::uuid FOR UPDATE`, orderID).Scan(&knownID, &method, &paymentStatus, &currency, &total, &storedSession)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "unknown order")
		return
	}
	if err != nil {
		fail(w, 503, "payment database unavailable")
		return
	}
	if method != "stripe" || currency != strings.ToUpper(session.Currency) || total != session.AmountTotal {
		fail(w, 400, "payment amount or currency mismatch")
		return
	}
	if storedSession != session.ID {
		// A freshly created provider session might notify before it is persisted; retry safely.
		fail(w, 503, "payment session not yet registered")
		return
	}
	tag, err := tx.Exec(ctx, `INSERT INTO payment_webhook_events(provider,event_id,event_type,order_id)
 VALUES('stripe',$1,$2,$3::uuid) ON CONFLICT(provider,event_id) DO NOTHING`, event.ID, event.Type, knownID)
	if err != nil {
		fail(w, 503, "payment database unavailable")
		return
	}
	if tag.RowsAffected() == 0 {
		w.WriteHeader(204)
		return
	}
	if paymentStatus != "paid" {
		switch event.Type {
		case "checkout.session.completed", "checkout.session.async_payment_succeeded":
			if session.PaymentStatus == "paid" {
				_, err = tx.Exec(ctx, `UPDATE orders SET payment_status='paid',order_status='placed',
     stripe_payment_intent_id=NULLIF($2,''),updated_at=now() WHERE id=$1::uuid`, knownID, session.PaymentIntent)
			}
		case "checkout.session.async_payment_failed", "checkout.session.expired":
			_, err = tx.Exec(ctx, `UPDATE orders SET payment_status='failed',order_status='payment_failed',
    updated_at=now() WHERE id=$1::uuid`, knownID)
		}
		if err != nil {
			fail(w, 503, "payment database unavailable")
			return
		}
	}
	if err = tx.Commit(ctx); err != nil {
		fail(w, 503, "payment database unavailable")
		return
	}
	w.WriteHeader(204)
}

// WebhookSignature provides a deterministic fixture helper for tests.
func WebhookSignature(payload []byte, secret string, timestamp int64) string {
	signed := strconv.FormatInt(timestamp, 10) + "." + string(payload)
	h := hmac.New(sha256.New, []byte(secret))
	_, _ = h.Write([]byte(signed))
	return "t=" + strconv.FormatInt(timestamp, 10) + ",v1=" + hex.EncodeToString(h.Sum(nil))
}

// Ensure checkout is configured with valid nonnegative financial settings.
func (s Service) Validate() error { return s.validate() }
