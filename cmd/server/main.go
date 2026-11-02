package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"schedule/internal/api"
	"schedule/internal/config"
	"schedule/internal/store"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {

	cfg := config.Load()
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pc, e := pgxpool.ParseConfig(cfg.DSN)
	if e != nil {
		log.Error("database config", "error", e)
		os.Exit(1)
	}

	var pool *pgxpool.Pool
	for range 30 {
		pool, e = pgxpool.NewWithConfig(ctx, pc)
		if e == nil {
			e = pool.Ping(ctx)
			if e == nil {
				break
			}
			pool.Close()
		}
		select {
		case <-ctx.Done():
			os.Exit(1)
		case <-time.After(time.Second):
		}
	}

	if e != nil {
		log.Error("database unavailable", "error", e)
		os.Exit(1)
	}

	defer pool.Close()

	e = store.Migrate(ctx, pool, cfg.Migrations)
	if e != nil {
		log.Error("migration failed", "error", e)
		os.Exit(1)
	}

	srv := &http.Server{Addr: cfg.Addr, Handler: api.New(pool, cfg, log), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		log.Info("server started", "addr", cfg.Addr)
		e := srv.ListenAndServe()
		if e != nil && !errors.Is(e, http.ErrServerClosed) {
			log.Error("server error", "error", e)
			stop()
		}
	}()
	<-ctx.Done()

	c, cancel := context.WithTimeout(context.Background(), cfg.Shutdown)
	defer cancel()

	e = srv.Shutdown(c)
	if e != nil {
		_ = srv.Close()
	}
	log.Info("server stopped")
}
