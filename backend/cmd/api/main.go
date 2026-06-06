package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"daily-speaking-practice/backend/internal/db"
	"daily-speaking-practice/backend/internal/httpapi"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	requireSSL := normalizeBool(os.Getenv("DATABASE_SSL"))
	database, err := db.Connect(ctx, databaseURL, requireSSL)
	if err != nil {
		log.Fatalf("database connect failed: %v", err)
	}
	defer database.Close()

	if err := database.Migrate(ctx); err != nil {
		log.Fatalf("database migration failed: %v", err)
	}

	addr := envDefault("APP_ADDR", ":3000")
	nextURL := envDefault("NEXT_UPSTREAM_URL", "http://127.0.0.1:3001")
	server := &http.Server{
		Addr:              addr,
		Handler:           httpapi.NewServer(httpapi.Config{DB: database, NextURL: nextURL}).Handler(),
		ReadHeaderTimeout: 15 * time.Second,
	}

	go func() {
		log.Printf("daily-speaking Go gateway listening on %s, proxying Next to %s", addr, nextURL)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server failed: %v", err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("server shutdown failed: %v", err)
	}
}

func normalizeBool(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	return normalized == "1" || normalized == "true" || normalized == "yes" || normalized == "on" || normalized == "require"
}

func envDefault(name string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}
