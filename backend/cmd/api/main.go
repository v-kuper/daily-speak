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

	"daily-speaking-practice/backend/internal/auth"
	"daily-speaking-practice/backend/internal/db"
	"daily-speaking-practice/backend/internal/httpapi"
)

func main() {
	cors, err := httpapi.ParseCORSConfig(os.Getenv("CORS_ALLOWED_ORIGINS"))
	if err != nil {
		log.Fatalf("CORS configuration failed: %v", err)
	}
	sessionCookie, err := auth.CookieConfigFromEnv()
	if err != nil {
		log.Fatalf("session cookie configuration failed: %v", err)
	}

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
	apiServer := httpapi.NewServer(httpapi.Config{DB: database, CORS: cors, SessionCookie: sessionCookie})
	apiServer.StartBackgroundWorkers(ctx)
	server := &http.Server{
		Addr:              addr,
		Handler:           apiServer.Handler(),
		ReadHeaderTimeout: 15 * time.Second,
	}

	go func() {
		log.Printf("daily-speaking API listening on %s", addr)
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
