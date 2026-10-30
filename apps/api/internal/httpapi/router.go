package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/justinangeloperez327/go-restaurant/apps/api/internal/auth"
	"github.com/justinangeloperez327/go-restaurant/apps/api/internal/catalog"
	"github.com/justinangeloperez327/go-restaurant/apps/api/internal/customer"
	"github.com/justinangeloperez327/go-restaurant/apps/api/internal/delivery"
	"github.com/justinangeloperez327/go-restaurant/apps/api/internal/operations"
	"github.com/justinangeloperez327/go-restaurant/apps/api/internal/orders"
	"github.com/justinangeloperez327/go-restaurant/apps/api/internal/subscriptions"
)

type databaseCheck func(context.Context) error

type statusResponse struct {
	Status   string `json:"status"`
	Database string `json:"database,omitempty"`
}

// NewRouter retains the Group 1 health-only router for independent tests.
func NewRouter(check databaseCheck) http.Handler {
	return NewAppRouter(check, nil, "")
}

func NewAppRouter(check databaseCheck, service *auth.Service, webOrigin string, catalogs ...*catalog.Service) http.Handler {
	return NewCustomerAppRouter(check, service, webOrigin, nil, catalogs...)
}

func NewCustomerAppRouter(check databaseCheck, service *auth.Service, webOrigin string, customers *customer.Service, catalogs ...*catalog.Service) http.Handler {
	return NewOrdersAppRouter(check, service, webOrigin, customers, nil, catalogs...)
}
func NewOrdersAppRouter(check databaseCheck, service *auth.Service, webOrigin string, customers *customer.Service, orderService *orders.Service, catalogs ...*catalog.Service) http.Handler {
	return NewSubscriptionsAppRouter(check, service, webOrigin, customers, orderService, nil, catalogs...)
}
func NewSubscriptionsAppRouter(check databaseCheck, service *auth.Service, webOrigin string, customers *customer.Service, orderService *orders.Service, subscriptionsService *subscriptions.Service, catalogs ...*catalog.Service) http.Handler {
	return NewDeliveryAppRouter(check, service, webOrigin, customers, orderService, subscriptionsService, nil, catalogs...)
}

func NewDeliveryAppRouter(check databaseCheck, service *auth.Service, webOrigin string, customers *customer.Service, orderService *orders.Service, subscriptionsService *subscriptions.Service, deliveryService *delivery.Service, catalogs ...*catalog.Service) http.Handler {
	return NewOperationsAppRouter(check, service, webOrigin, customers, orderService, subscriptionsService, deliveryService, nil, catalogs...)
}
func NewOperationsAppRouter(check databaseCheck, service *auth.Service, webOrigin string, customers *customer.Service, orderService *orders.Service, subscriptionsService *subscriptions.Service, deliveryService *delivery.Service, operationService *operations.Service, catalogs ...*catalog.Service) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(securityHeaders)
	r.Use(cors(webOrigin))

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
	})
	r.Get("/readyz", func(w http.ResponseWriter, req *http.Request) {
		ctx, cancel := context.WithTimeout(req.Context(), 2*time.Second)
		defer cancel()
		if check == nil || check(ctx) != nil {
			writeJSON(w, http.StatusServiceUnavailable, statusResponse{
				Status: "unavailable", Database: "down",
			})
			return
		}
		writeJSON(w, http.StatusOK, statusResponse{Status: "ready", Database: "up"})
	})
	if service != nil {
		r.Route("/api/v1", func(api chi.Router) {
			service.Mount(api)
			if len(catalogs) > 0 && catalogs[0] != nil {
				catalogs[0].Mount(api)
			}
			if customers != nil {
				customers.Mount(api)
			}
			if orderService != nil {
				orderService.Mount(api)
			}
			if subscriptionsService != nil {
				subscriptionsService.Mount(api)
			}
			if deliveryService != nil {
				deliveryService.Mount(api)
			}
			if operationService != nil {
				operationService.Mount(api)
			}
		})
	}
	return r
}

func cors(allowedOrigin string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" {
				w.Header().Add("Vary", "Origin")
			}
			if allowedOrigin != "" && origin == allowedOrigin {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Idempotency-Key, Last-Event-ID")
				if r.Method == http.MethodOptions {
					w.WriteHeader(http.StatusNoContent)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func writeJSON(w http.ResponseWriter, code int, value statusResponse) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}

func securityHeaders(next http.Handler) http.Handler {
 return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  w.Header().Set("X-Content-Type-Options","nosniff")
  w.Header().Set("X-Frame-Options","DENY")
  w.Header().Set("Referrer-Policy","no-referrer")
  w.Header().Set("Permissions-Policy","camera=(), microphone=(), geolocation=()")
  w.Header().Set("Cache-Control","no-store")
  if r.TLS!=nil {w.Header().Set("Strict-Transport-Security","max-age=31536000")}
  next.ServeHTTP(w,r)
 })
}
