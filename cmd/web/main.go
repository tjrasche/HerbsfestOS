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
	"github.com/tjrasche/HerbsfestOS/internal/feedbacknote"
	"github.com/tjrasche/HerbsfestOS/internal/ui"
)

func main() {
	if err := run(); err != nil {
		slog.Error("application stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	migrate := flag.Bool("migrate", false, "apply the initial schema and exit")
	flag.Parse()
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
		return db.WithContext(migrationCtx).AutoMigrate(&feedbacknote.FeedbackNote{})
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
	feedbacknote.NewHandler(feedbacknote.NewService(feedbacknote.NewRepository(db))).Register(private)
	ui.Register(private)
	mux := http.NewServeMux()
	authHandler.Register(mux)
	mux.Handle("/", authHandler.Protect(private, ui.AccessDenied()))
	mux.Handle("GET /static/", ui.Assets())
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
