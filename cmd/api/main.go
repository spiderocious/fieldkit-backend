// Command api boots the fieldkit HTTP server.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spiderocious/devfest-akure-2026/fieldkit/backend/internal/app"
	"github.com/spiderocious/devfest-akure-2026/fieldkit/backend/internal/db"
	"github.com/spiderocious/devfest-akure-2026/fieldkit/backend/internal/env"
	"github.com/spiderocious/devfest-akure-2026/fieldkit/backend/internal/logx"
	"github.com/spiderocious/devfest-akure-2026/fieldkit/backend/internal/middleware"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := env.Load()
	if err != nil {
		return err
	}
	log := logx.New(cfg.LogLevel)
	slog.SetDefault(log)

	// Production-only checks live here, not in env.Load, so dev can boot
	// without them.
	if cfg.IsProduction() && cfg.DemoControl {
		log.Warn("DEMO_CONTROL is enabled in production; the API can be made to fail on demand")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	engine, err := app.Engine(app.Deps{Config: cfg, Pool: pool, Log: log, Chaos: middleware.NewChaos()})
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           engine,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		log.Info("listening", "port", cfg.Port, "env", cfg.Env, "demo_control", cfg.DemoControl)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
