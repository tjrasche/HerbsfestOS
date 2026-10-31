package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tjrasche/HerbsfestOS/internal/auth"
	"github.com/tjrasche/HerbsfestOS/internal/database"
	"github.com/tjrasche/HerbsfestOS/internal/openfga"
	"github.com/tjrasche/HerbsfestOS/internal/scheduling"
	"github.com/tjrasche/HerbsfestOS/internal/ui"
)

func main() {
	if err := run(); err != nil {
		slog.Error("application stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	migrate := flag.Bool("migrate", false, "apply versioned maintenance migrations and exit")
	bootstrap := flag.Bool("bootstrap-organizer", false, "grant an explicitly identified login during maintenance")
	checkOpenFGA := flag.Bool("check-openfga", false, "verify OpenFGA connectivity and API authentication, then exit")
	flag.Parse()
	if (*migrate && *checkOpenFGA) || (*bootstrap && (*migrate || *checkOpenFGA)) {
		return errors.New("-migrate and -check-openfga are mutually exclusive")
	}
	var fgaClient *openfga.Client
	// Database migrations require no OpenFGA configuration or API credential.
	if !*migrate && !*bootstrap {
		cfg, err := openfga.ConfigFromEnv(os.Getenv)
		if err != nil {
			return err
		}
		if cfg.APIURL != "" || *checkOpenFGA {
			fgaClient, err = openfga.New(cfg)
			if err != nil {
				return err
			}
			defer fgaClient.Close()
			if *checkOpenFGA {
				if err := fgaClient.Ping(context.Background()); err != nil {
					return err
				}
				slog.Info("OpenFGA connectivity verified")
				return nil
			}
			// Model installation is an explicit operator step.
			slog.Info("OpenFGA client configured", "model_configured", cfg.StoreID != "")
		}
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL is required")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	db, pool, err := database.Open(connectCtx, dsn)
	cancel()
	if err != nil {
		return err
	}
	defer pool.Close()
	if *migrate {
		migrationCtx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		return scheduling.Migrate(migrationCtx, db, scheduling.LegacyEdition{Label: os.Getenv("LEGACY_EDITION_LABEL"), StartDay: os.Getenv("LEGACY_START_DAY"), EndDay: os.Getenv("LEGACY_END_DAY")})
	}

	if *bootstrap {
		return scheduling.Bootstrap(ctx, db, os.Getenv("BOOTSTRAP_ISSUER"), os.Getenv("BOOTSTRAP_SUBJECT"))
	}
	authConfig, err := auth.ConfigFromEnv(os.Getenv)
	if err != nil {
		return err
	}
	authHandler, err := auth.New(ctx, authConfig)
	if err != nil {
		return err
	}
	if authConfig.Mode == "development" {
		slog.Warn("authentication bypass enabled for local development")
	}
	private := http.NewServeMux()
	mux := newMux(authHandler.Protect(private, ui.AccessDenied()))
	scheduling.NewHandler(scheduling.New(db, fgaClient)).Register(private, mux, authHandler.Optional)
	authHandler.Register(mux)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := pool.PingContext(ctx); err != nil {
			http.Error(w, "Database unavailable", http.StatusServiceUnavailable)
			return
		}
		var version int
		if err := db.WithContext(ctx).Raw("SELECT count(*) FROM schema_migrations WHERE version=1").Scan(&version).Error; err != nil || version != 1 {
			http.Error(w, "Schema migration required", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	server := &http.Server{
		Addr:              addr,
		Handler:           http.NewCrossOriginProtection().Handler(mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	errs := make(chan error, 1)
	go func() { errs <- server.ListenAndServe() }()
	slog.Info("listening", "address", addr)
	select {
	case err := <-errs:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP: %w", err)
		}
		return nil
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			server.Close()
			return fmt.Errorf("shutdown: %w", err)
		}
		return nil
	}
}
