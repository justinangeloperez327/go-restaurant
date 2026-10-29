package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/justinangeloperez327/go-restaurant/apps/api/internal/auth"
	"github.com/justinangeloperez327/go-restaurant/apps/api/internal/catalog"
	"github.com/justinangeloperez327/go-restaurant/apps/api/internal/config"
	"github.com/justinangeloperez327/go-restaurant/apps/api/internal/customer"
	"github.com/justinangeloperez327/go-restaurant/apps/api/internal/db"
	"github.com/justinangeloperez327/go-restaurant/apps/api/internal/httpapi"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		log.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("invalid database configuration", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	initCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := db.Migrate(initCtx, pool); err != nil {
		log.Error("database migration failed", "error", err)
		os.Exit(1)
	}
	service, err := auth.NewService(cfg, pool)
	if err != nil {
		log.Error("authentication configuration failed", "error", err)
		os.Exit(1)
	}

	// Safe across multiple replicas: every cleanup deletes only expired rows.
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := (auth.Store{DB: pool}).Prune(ctx); err != nil {
					log.Warn("authentication cleanup failed", "error", err)
				}
			}
		}
	}()

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           httpapi.NewCustomerAppRouter(pool.Ping, service, cfg.WebOrigin, &customer.Service{DB: pool, Auth: service, WebOrigin: cfg.WebOrigin}, &catalog.Service{DB: pool, Auth: service, WebOrigin: cfg.WebOrigin}),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	shutdownDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := server.Shutdown(shutdownCtx); err != nil {
				log.Error("graceful shutdown failed", "error", err)
			}
		case <-shutdownDone:
		}
	}()

	log.Info("api started", "port", cfg.Port)
	err = server.ListenAndServe()
	close(shutdownDone)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("api stopped unexpectedly", "error", err)
		os.Exit(1)
	}
}
