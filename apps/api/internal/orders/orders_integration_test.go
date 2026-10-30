package orders_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/justinangeloperez327/go-restaurant/apps/api/internal/auth"
	"github.com/justinangeloperez327/go-restaurant/apps/api/internal/config"
	"github.com/justinangeloperez327/go-restaurant/apps/api/internal/customer"
	"github.com/justinangeloperez327/go-restaurant/apps/api/internal/db"
	"github.com/justinangeloperez327/go-restaurant/apps/api/internal/httpapi"
	"github.com/justinangeloperez327/go-restaurant/apps/api/internal/orders"
)

type fixture struct {
	t                                                                       *testing.T
	pool                                                                    *pgxpool.Pool
	router                                                                  http.Handler
	userID, otherID, token, otherToken, restaurant, meal, addrID, otherAddr string
	stripeMock                                                              *httptest.Server
}

func random(t *testing.T) string {
	t.Helper()
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}
func newFixture(t *testing.T, withStripe bool) *fixture {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL required")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close() })
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	as, err := auth.NewService(config.Config{WebOrigin: "http://localhost:3000"}, pool)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, pool: pool}
	store := auth.Store{DB: pool}
	for _, item := range []struct{ id, token *string }{{&f.userID, &f.token}, {&f.otherID, &f.otherToken}} {
		id, err := store.ResolveIdentity(ctx, "google", random(t), "", "Test customer")
		if err != nil {
			t.Fatal(err)
		}
		*item.id = id
		t.Cleanup(func() {
			_, _ = pool.Exec(context.Background(), "DELETE FROM orders WHERE user_id=$1::uuid", id)
			_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id=$1::uuid", id)
		})
		*item.token = random(t)
		if err := store.NewSession(ctx, id, *item.token); err != nil {
			t.Fatal(err)
		}
	}
	slug := "order-" + random(t)[:12]
	if err := pool.QueryRow(ctx, `INSERT INTO restaurants(name,slug,currency,is_active)
 VALUES('Order Kitchen',$1,'AED',true) RETURNING id::text`, slug).Scan(&f.restaurant); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM restaurants WHERE id=$1::uuid", f.restaurant)
	})
	var category string
	if err := pool.QueryRow(ctx, `INSERT INTO menu_categories(restaurant_id,name,is_active)
 VALUES($1::uuid,'Lunch',true) RETURNING id::text`, f.restaurant).Scan(&category); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO meals(restaurant_id,category_id,name,price_minor,is_active,is_available)
 VALUES($1::uuid,$2::uuid,'Lunch meal',2450,true,true) RETURNING id::text`, f.restaurant, category).Scan(&f.meal); err != nil {
		t.Fatal(err)
	}
	for _, p := range []struct{ user, addr *string }{{&f.userID, &f.addrID}, {&f.otherID, &f.otherAddr}} {
		if err := pool.QueryRow(ctx, `INSERT INTO delivery_addresses(user_id,label,recipient_name,phone,country_code,
  city,area,street,building,apartment,instructions,is_default)
  VALUES($1::uuid,'Home','Customer','+971501234567','AE','Abu Dhabi','','Main Street','','','',true)
  RETURNING id::text`, *p.user).Scan(p.addr); err != nil {
			t.Fatal(err)
		}
	}
	var gw orders.StripeGateway
	if withStripe {
		f.stripeMock = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.Header.Get("Authorization"), "Basic ") {
				t.Error("missing Stripe auth")
			}
			if r.Header.Get("Idempotency-Key") == "" {
				t.Error("missing Stripe idempotency key")
			}
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			if r.Form.Get("line_items[0][price_data][unit_amount]") == "" {
				t.Error("missing amount")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"cs_test_12345","url":"https://checkout.stripe.com/c/pay/cs_test_12345"}`))
		}))
		t.Cleanup(f.stripeMock.Close)
		gw = orders.StripeClient{SecretKey: "sk_test_fake", WebhookSecret: "whsec_test", APIURL: f.stripeMock.URL, HTTP: f.stripeMock.Client()}
	}
	ord := &orders.Service{DB: pool, Auth: as, WebOrigin: "http://localhost:3000", Stripe: gw, AllowCOD: true, TaxBPS: 500, DeliveryFeeMinor: 400}
	f.router = httpapi.NewOrdersAppRouter(pool.Ping, as, "http://localhost:3000",
		&customer.Service{DB: pool, Auth: as, WebOrigin: "http://localhost:3000"}, ord)
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, `DELETE FROM payment_webhook_events WHERE order_id IN
   (SELECT id FROM orders WHERE user_id IN ($1::uuid,$2::uuid))`, f.userID, f.otherID)
		_, _ = pool.Exec(ctx, `DELETE FROM order_items WHERE order_id IN
   (SELECT id FROM orders WHERE user_id IN ($1::uuid,$2::uuid))`, f.userID, f.otherID)
		_, _ = pool.Exec(ctx, `DELETE FROM orders WHERE user_id IN ($1::uuid,$2::uuid)`, f.userID, f.otherID)
	})
	return f
}
func (f *fixture) call(method, path, token, key string, body any) (int, map[string]any) {
	f.t.Helper()
	var b []byte
	if body != nil {
		var err error
		b, err = json.Marshal(body)
		if err != nil {
			f.t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(b))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.AddCookie(&http.Cookie{Name: "gr_session", Value: token})
	}
	if method != "GET" {
		req.Header.Set("Origin", "http://localhost:3000")
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	var result map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &result)
	return rec.Code, result
}
func (f *fixture) expect(method, path, token, key string, body any, want int) map[string]any {
	f.t.Helper()
	code, v := f.call(method, path, token, key, body)
	if code != want {
		f.t.Fatalf("%s %s code=%d want=%d body=%v", method, path, code, want, v)
	}
	return v
}
func (f *fixture) add() {
	f.t.Helper()
	f.expect("POST", "/api/v1/cart/items", f.token, "", map[string]any{
		"meal_id": f.meal, "quantity": 2, "option_ids": []string{},
	}, 201)
}
func (f *fixture) webhook(t *testing.T, eventType, eventID, orderID, sessionID, payment, amount string, sign bool) int {
	t.Helper()
	raw := []byte(fmt.Sprintf(`{"id":%q,"type":%q,"data":{"object":{"id":%q,
 "client_reference_id":%q,"metadata":{"order_id":%q},"amount_total":%s,
 "currency":"aed","payment_status":%q,"payment_intent":"pi_test_123"}}}`,
		eventID, eventType, sessionID, orderID, orderID, amount, payment))
	req := httptest.NewRequest("POST", "/api/v1/webhooks/stripe", bytes.NewReader(raw))
	sig := orders.WebhookSignature(raw, "whsec_test", time.Now().Unix())
	if !sign {
		sig = "t=12345,v1=invalid"
	}
	req.Header.Set("Stripe-Signature", sig)
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec.Code
}
func TestCODCheckoutSnapshotIdempotencyAndOwnership(t *testing.T) {
	f := newFixture(t, false)
	f.add()
	quote := f.expect("GET", "/api/v1/checkout/quote?address_id="+f.addrID, f.token, "", nil, 200)
	// AED 49.00 + 4.00 delivery + 5% configured tax = 55.65
	if quote["subtotal_minor"] != float64(4900) || quote["tax_minor"] != float64(265) ||
		quote["total_minor"] != float64(5565) {
		t.Fatalf("incorrect quote %v", quote)
	}
	if code, _ := f.call("GET", "/api/v1/checkout/quote?address_id="+f.otherAddr, f.token, "", nil); code != 404 {
		t.Fatalf("other user's address accessible: %d", code)
	}
	req := map[string]any{"address_id": f.addrID, "payment_method": "cod", "notes": "Leave at reception"}
	key := "checkout_" + random(t)
	first := f.expect("POST", "/api/v1/checkout", f.token, key, req, 200)
	id := first["order_id"].(string)
	if first["payment_status"] != "unpaid" {
		t.Fatal("COD must not be marked paid")
	}
	again := f.expect("POST", "/api/v1/checkout", f.token, key, req, 200)
	if again["order_id"] != id {
		t.Fatal("idempotency returned different order")
	}
	mismatch := map[string]any{"address_id": f.addrID, "payment_method": "cod", "notes": "changed"}
	f.expect("POST", "/api/v1/checkout", f.token, key, mismatch, 409)
	order := f.expect("GET", "/api/v1/orders/"+id, f.token, "", nil, 200)
	if order["total_minor"] != float64(5565) || order["order_status"] != "placed" {
		t.Fatalf("wrong order snapshot: %v", order)
	}
	if len(order["items"].([]any)) != 1 {
		t.Fatal("missing snapshot line")
	}
	if code, _ := f.call("GET", "/api/v1/orders/"+id, f.otherToken, "", nil); code != 404 {
		t.Fatalf("order leaked: %d", code)
	}
	f.expect("GET", "/api/v1/orders", f.token, "", nil, 200)
	cart := f.expect("GET", "/api/v1/cart", f.token, "", nil, 200)
	if len(cart["items"].([]any)) != 0 {
		t.Fatal("cart not cleared atomically")
	}
	if _, err := f.pool.Exec(context.Background(), "UPDATE meals SET price_minor=9999 WHERE id=$1::uuid", f.meal); err != nil {
		t.Fatal(err)
	}
	saved := f.expect("GET", "/api/v1/orders/"+id, f.token, "", nil, 200)
	if saved["subtotal_minor"] != float64(4900) {
		t.Fatalf("order price was not snapshotted: %v", saved)
	}
	f.expect("POST", "/api/v1/checkout", f.token, "short", req, 400)
}
func TestStripeWebhookOnlyVerifiedPayments(t *testing.T) {
	f := newFixture(t, true)
	f.add()
	key := "checkout_" + random(t)
	req := map[string]any{"address_id": f.addrID, "payment_method": "stripe", "notes": ""}
	result := f.expect("POST", "/api/v1/checkout", f.token, key, req, 200)
	id := result["order_id"].(string)
	if !strings.HasPrefix(result["redirect_url"].(string), "https://checkout.stripe.com/") {
		t.Fatal("invalid redirect")
	}
	if retry := f.expect("POST", "/api/v1/checkout", f.token, key, req, 200); retry["order_id"] != id {
		t.Fatal("Stripe idempotency failed")
	}
	if got := f.webhook(t, "checkout.session.completed", "evt_invalid", id, "cs_test_12345", "paid", "5565", false); got != 400 {
		t.Fatalf("unsigned webhook accepted: %d", got)
	}
	if got := f.webhook(t, "checkout.session.completed", "evt_wrong", id, "cs_test_12345", "paid", "1", true); got != 400 {
		t.Fatalf("wrong amount accepted: %d", got)
	}
	if got := f.webhook(t, "checkout.session.completed", "evt_correct", id, "cs_test_12345", "paid", "5565", true); got != 204 {
		t.Fatalf("paid event not processed: %d", got)
	}
	if got := f.webhook(t, "checkout.session.completed", "evt_correct", id, "cs_test_12345", "paid", "5565", true); got != 204 {
		t.Fatalf("duplicate webhook not idempotent: %d", got)
	}
	if got := f.webhook(t, "checkout.session.async_payment_failed", "evt_late", id, "cs_test_12345", "unpaid", "5565", true); got != 204 {
		t.Fatalf("late failure rejected: %d", got)
	}
	o := f.expect("GET", "/api/v1/orders/"+id, f.token, "", nil, 200)
	if o["payment_status"] != "paid" || o["order_status"] != "placed" {
		t.Fatalf("webhook didn't reconcile payment: %v", o)
	}
	var events int
	if err := f.pool.QueryRow(context.Background(), "SELECT count(*) FROM payment_webhook_events WHERE order_id=$1::uuid", id).Scan(&events); err != nil || events != 2 {
		t.Fatalf("webhook dedup rows=%d err=%v", events, err)
	}
}
func TestCheckoutInvalidCartAndReplay(t *testing.T) {
	f := newFixture(t, false)
	req := map[string]any{"address_id": f.addrID, "payment_method": "cod", "notes": ""}
	key := "checkout_" + random(t)
	f.expect("POST", "/api/v1/checkout", f.token, key, req, 409)
	f.add()
	if _, err := f.pool.Exec(context.Background(), "UPDATE meals SET is_available=false WHERE id=$1::uuid", f.meal); err != nil {
		t.Fatal(err)
	}
	f.expect("POST", "/api/v1/checkout", f.token, key, req, 409)
	if _, err := f.pool.Exec(context.Background(), "UPDATE meals SET is_available=true WHERE id=$1::uuid", f.meal); err != nil {
		t.Fatal(err)
	}
	if code, _ := f.call("POST", "/api/v1/checkout", f.token, key, map[string]any{"address_id": f.otherAddr, "payment_method": "cod"}); code != 404 {
		t.Fatalf("cross-owner address accepted: %d", code)
	}
}

func TestCheckoutDoesNotRedirectToUnregisteredStripeSession(t *testing.T) {
	f := newFixture(t, true)
	f.add()
	key := "checkout_" + random(t)
	input := map[string]any{"address_id": f.addrID, "payment_method": "stripe", "notes": ""}
	response := f.expect("POST", "/api/v1/checkout", f.token, key, input, 200)
	id := response["order_id"].(string)
	// Simulate an interrupted provider-session write that left the order pointing
	// at an unrelated session ID and no resumable URL.
	_, err := f.pool.Exec(context.Background(), `UPDATE orders SET stripe_session_id='cs_test_conflict',
 stripe_checkout_url='' WHERE id=$1::uuid`, id)
	if err != nil {
		t.Fatal(err)
	}
	f.expect("POST", "/api/v1/checkout", f.token, key, input, 409)
	var stored string
	err = f.pool.QueryRow(context.Background(),
		"SELECT stripe_session_id FROM orders WHERE id=$1::uuid", id).Scan(&stored)
	if err != nil || stored != "cs_test_conflict" {
		t.Fatalf("session overwritten: %q, %v", stored, err)
	}
}
