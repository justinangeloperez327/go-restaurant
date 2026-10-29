package operations_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/justinangeloperez327/go-restaurant/apps/api/internal/auth"
	"github.com/justinangeloperez327/go-restaurant/apps/api/internal/config"
	"github.com/justinangeloperez327/go-restaurant/apps/api/internal/db"
	"github.com/justinangeloperez327/go-restaurant/apps/api/internal/httpapi"
	"github.com/justinangeloperez327/go-restaurant/apps/api/internal/operations"
)

type fixture struct {
	t                                                                        *testing.T
	db                                                                       *pgxpool.Pool
	handler                                                                  http.Handler
	service                                                                  *operations.Service
	manager, second, admin, customer, rider                                  string
	managerToken, secondToken, adminToken, customerToken                     string
	restaurant, otherRestaurant                                              string
	codOrder, paidOrder, failedOrder, otherOrder, deliveryID, subscriptionID string
}

func random(t *testing.T) string {
	t.Helper()
	raw := make([]byte, 20)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(raw)
}
func makeFixture(t *testing.T) *fixture {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("requires PostgreSQL DATABASE_URL")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err = db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	as, err := auth.NewService(config.Config{WebOrigin: "http://localhost:3000"}, pool)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, db: pool, service: &operations.Service{DB: pool, Auth: as, WebOrigin: "http://localhost:3000"}}
	f.handler = httpapi.NewOperationsAppRouter(pool.Ping, as, "http://localhost:3000", nil, nil, nil, nil, f.service)
	store := auth.Store{DB: pool}
	for _, p := range []struct {
		id, token *string
		role      string
	}{
		{&f.manager, &f.managerToken, "restaurant_manager"},
		{&f.second, &f.secondToken, "restaurant_manager"},
		{&f.admin, &f.adminToken, "admin"},
		{&f.customer, &f.customerToken, "customer"},
		{&f.rider, nil, "rider"},
	} {
		id, err := store.ResolveIdentity(ctx, "google", random(t), "", "Manager")
		if err != nil {
			t.Fatal(err)
		}
		*p.id = id
		if p.token != nil {
			*p.token = random(t)
			if err = store.NewSession(ctx, id, *p.token); err != nil {
				t.Fatal(err)
			}
		}
		if p.role != "customer" {
			if _, err = pool.Exec(ctx, "INSERT INTO user_roles(user_id,role_name) VALUES($1::uuid,$2)", id, p.role); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, ptr := range []*string{&f.restaurant, &f.otherRestaurant} {
		err = pool.QueryRow(ctx, `INSERT INTO restaurants(name,slug,currency,is_active)
  VALUES('Operations Kitchen',$1,'AED',true) RETURNING id::text`, "ops-"+random(t)[:12]).Scan(ptr)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err = pool.Exec(ctx, `INSERT INTO restaurant_staff(restaurant_id,user_id) VALUES
 ($1::uuid,$2::uuid),($3::uuid,$4::uuid)`, f.restaurant, f.manager, f.otherRestaurant, f.second); err != nil {
		t.Fatal(err)
	}
	insertOrder := func(restaurant, method, status, payment string, total int64) string {
		t.Helper()
		var id string
		err = pool.QueryRow(ctx, `INSERT INTO orders(user_id,restaurant_id,delivery_address,idempotency_key,
   payment_method,order_status,payment_status,currency,subtotal_minor,
   delivery_fee_minor,tax_minor,total_minor)
   VALUES($1::uuid,$2::uuid,'{"city":"Abu Dhabi","street":"Test"}'::jsonb,
   $3,$4,$5,$6,'AED',$7,0,0,$7) RETURNING id::text`,
			f.customer, restaurant, "test-"+random(t), method, status, payment, total).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	f.codOrder = insertOrder(f.restaurant, "cod", "placed", "unpaid", 2500)
	f.paidOrder = insertOrder(f.restaurant, "stripe", "placed", "paid", 3500)
	f.failedOrder = insertOrder(f.restaurant, "stripe", "payment_failed", "failed", 1800)
	f.otherOrder = insertOrder(f.otherRestaurant, "cod", "placed", "unpaid", 9999)
	err = pool.QueryRow(ctx, `INSERT INTO delivery_jobs(order_id,restaurant_id,rider_id,status,completed_at)
 VALUES($1::uuid,$2::uuid,$3::uuid,'failed',now()) RETURNING id::text`,
		f.codOrder, f.restaurant, f.rider).Scan(&f.deliveryID)
	if err != nil {
		t.Fatal(err)
	}
	var addressID string
	err = pool.QueryRow(ctx, `INSERT INTO delivery_addresses(user_id,label,recipient_name,phone,country_code,city,
 area,street,building,apartment,instructions,is_default) VALUES
 ($1::uuid,'Home','Test','+971501234567','AE','Abu Dhabi','','Test','','','',true) RETURNING id::text`,
		f.customer).Scan(&addressID)
	if err != nil {
		t.Fatal(err)
	}
	err = pool.QueryRow(ctx, `INSERT INTO meal_subscriptions(user_id,restaurant_id,address_id,weekday_mask,
 delivery_time,next_delivery_date,next_run_at,current_period_end,auto_renew,max_total_minor)
 VALUES($1::uuid,$2::uuid,$3::uuid,2,'12:00',current_date+interval '1 day',now(),
 current_date+interval '7 days',true,10000) RETURNING id::text`,
		f.customer, f.restaurant, addressID).Scan(&f.subscriptionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO meal_subscription_occurrences(subscription_id,delivery_date,status,reason)
 VALUES($1::uuid,current_date,'failed','price_cap_exceeded')`, f.subscriptionID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		clean := context.Background()
		_, _ = pool.Exec(clean, `DELETE FROM operation_alert_reads WHERE alert_id IN (
  SELECT id FROM operation_alerts WHERE restaurant_id IN ($1::uuid,$2::uuid))`, f.restaurant, f.otherRestaurant)
		_, _ = pool.Exec(clean, "DELETE FROM operation_alerts WHERE restaurant_id IN ($1::uuid,$2::uuid)", f.restaurant, f.otherRestaurant)
		_, _ = pool.Exec(clean, "DELETE FROM meal_subscription_occurrences WHERE subscription_id=$1::uuid", f.subscriptionID)
		_, _ = pool.Exec(clean, "DELETE FROM meal_subscriptions WHERE id=$1::uuid", f.subscriptionID)
		_, _ = pool.Exec(clean, "DELETE FROM delivery_events WHERE delivery_id=$1::uuid", f.deliveryID)
		_, _ = pool.Exec(clean, "DELETE FROM delivery_jobs WHERE id=$1::uuid", f.deliveryID)
		for _, id := range []string{f.codOrder, f.paidOrder, f.failedOrder, f.otherOrder} {
			_, _ = pool.Exec(clean, "DELETE FROM orders WHERE id=$1::uuid", id)
		}
		for _, id := range []string{f.manager, f.second, f.admin, f.customer, f.rider} {
			_, _ = pool.Exec(clean, "DELETE FROM users WHERE id=$1::uuid", id)
		}
		for _, id := range []string{f.restaurant, f.otherRestaurant} {
			_, _ = pool.Exec(clean, "DELETE FROM restaurants WHERE id=$1::uuid", id)
		}
	})
	return f
}
func (f *fixture) request(method, path, token, origin string) (int, map[string]any, string) {
	f.t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(nil))
	if token != "" {
		req.AddCookie(&http.Cookie{Name: "gr_session", Value: token})
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	out := httptest.NewRecorder()
	f.handler.ServeHTTP(out, req)
	var body map[string]any
	_ = json.Unmarshal(out.Body.Bytes(), &body)
	return out.Code, body, out.Body.String()
}
func (f *fixture) expect(method, path, token string, want int) map[string]any {
	f.t.Helper()
	code, body, _ := f.request(method, path, token, "http://localhost:3000")
	if code != want {
		f.t.Fatalf("%s %s status=%d want=%d body=%v", method, path, code, want, body)
	}
	return body
}
func TestRestaurantReportingAndExport(t *testing.T) {
	f := makeFixture(t)
	base := "/api/v1/admin/operations/report?restaurant_id=" + f.restaurant
	f.expect("GET", base, f.customerToken, 403)
	f.expect("GET", base, f.secondToken, 403)
	f.expect("GET", base, f.managerToken, 200)
	f.expect("GET", base, f.adminToken, 200)
	f.expect("GET", base+"&from=2020-01-01&to=2021-01-01", f.managerToken, 400)
	f.expect("GET", base+"&from=2026-02-30", f.managerToken, 400)
	data := f.expect("GET", base, f.managerToken, 200)
	m := data["metrics"].(map[string]any)
	if m["orders_total"] != float64(3) || m["orders_placed"] != float64(2) ||
		m["booked_minor"] != float64(6000) || m["paid_minor"] != float64(3500) ||
		m["cod_uncollected_minor"] != float64(2500) || m["delivery_failed"] != float64(1) ||
		m["subscriptions_active"] != float64(1) || m["subscription_failures"] != float64(1) ||
		m["unassigned_orders"] != float64(1) {
		t.Fatalf("incorrect scoped metrics: %v", m)
	}
	daily := data["days"].([]any)
	if len(daily) != 1 || daily[0].(map[string]any)["booked_minor"] != float64(6000) {
		t.Fatalf("daily report invalid: %v", daily)
	}
	code, _, csv := f.request("GET", strings.Replace(base, "/report?", "/report.csv?", 1), f.managerToken, "")
	if code != 200 || !strings.Contains(csv, "cod_uncollected_aed_fils") || !strings.Contains(csv, "6000,3500,2500") {
		t.Fatalf("CSV invalid code=%d body=%s", code, csv)
	}
	if code, _, _ := f.request("GET", strings.Replace(base, "/report?", "/report.csv?", 1), f.secondToken, ""); code != 403 {
		t.Fatalf("cross-restaurant CSV leak code=%d", code)
	}
	// Change assigned restaurant manager to a stranger and ensure same-session permissions are rechecked.
	if _, err := f.db.Exec(context.Background(), `DELETE FROM restaurant_staff WHERE restaurant_id=$1::uuid AND user_id=$2::uuid`, f.restaurant, f.manager); err != nil {
		t.Fatal(err)
	}
	f.expect("GET", base, f.managerToken, 403)
}
func TestAlertsIdempotencyAndIndividualReadState(t *testing.T) {
	f := makeFixture(t)
	ctx := context.Background()
	n, err := f.service.SyncAlerts(ctx)
	if err != nil || n < 6 {
		t.Fatalf("alert sync failed count=%d err=%v", n, err)
	}
	again, err := f.service.SyncAlerts(ctx)
	if err != nil || again != 0 {
		t.Fatalf("alert reconciliation not idempotent count=%d err=%v", again, err)
	}
	url := "/api/v1/admin/operations/alerts?restaurant_id=" + f.restaurant
	data := f.expect("GET", url, f.managerToken, 200)
	if data["unread"] != float64(6) {
		t.Fatalf("unexpected scoped unread alerts: %v", data)
	}
	list := data["alerts"].([]any)
	if len(list) != 6 {
		t.Fatalf("expected 6 restaurant-scoped alerts: %v", list)
	}
	f.expect("GET", url, f.secondToken, 403)
	other := f.expect("GET", "/api/v1/admin/operations/alerts?restaurant_id="+f.otherRestaurant, f.secondToken, 200)
	if other["unread"] != float64(1) {
		t.Fatalf("other restaurant alert incorrectly scoped: %v", other)
	}
	id := list[0].(map[string]any)["id"].(string)
	path := "/api/v1/admin/operations/alerts/" + id + "/read?restaurant_id=" + f.restaurant
	f.expect("PUT", path, f.secondToken, 403)
	if code, _, _ := f.request("PUT", path, f.managerToken, "https://other.example"); code != 403 {
		t.Fatalf("CSRF allowed: %d", code)
	}
	f.expect("PUT", path, f.managerToken, 204)
	f.expect("PUT", path, f.managerToken, 204)
	if got := f.expect("GET", url, f.managerToken, 200)["unread"]; got != float64(5) {
		t.Fatalf("read receipt not saved: %v", got)
	}
	if got := f.expect("GET", url, f.adminToken, 200)["unread"]; got != float64(6) {
		t.Fatalf("manager read receipt leaked to admin: %v", got)
	}
	f.expect("PUT", path, f.customerToken, 403)
	// Revocation must immediately revoke inbox access, without requiring logout.
	if _, err := f.db.Exec(ctx, "UPDATE users SET is_active=false WHERE id=$1::uuid", f.manager); err != nil {
		t.Fatal(err)
	}
	f.expect("GET", url, f.managerToken, 401)
}
