package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"daily-speaking-practice/backend/internal/db"
	"daily-speaking-practice/backend/internal/httpapi"
	"daily-speaking-practice/backend/internal/storage"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	requireSSL, err := parseDatabaseSSL(os.Getenv("DATABASE_SSL"))
	if err != nil {
		log.Fatalf("database SSL configuration failed: %v", err)
	}
	database, err := db.Connect(ctx, strings.TrimSpace(os.Getenv("DATABASE_URL")), requireSSL)
	if err != nil {
		log.Fatalf("database connect failed: %v", err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		log.Fatalf("database migration failed: %v", err)
	}
	mediaConfig, err := storage.ConfigFromEnv()
	if err != nil {
		log.Fatalf("media storage configuration failed: %v", err)
	}
	mediaStore, err := storage.New(ctx, mediaConfig)
	if err != nil {
		log.Fatalf("media storage initialization failed: %v", err)
	}

	config, err := httpapi.WorkerConfigFromEnv()
	if err != nil {
		log.Fatalf("worker configuration failed: %v", err)
	}
	worker := httpapi.NewServer(httpapi.Config{
		DB: database, MediaStore: mediaStore, MediaBucket: mediaConfig.S3Bucket,
		MediaPartSize: mediaConfig.MultipartPartSize, MediaPresignTTL: mediaConfig.PresignTTL,
	})
	log.Printf("daily-speaking worker started")
	if err := worker.RunWorkers(ctx, config); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("worker failed: %v", err)
	}
}

func parseDatabaseSSL(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "0", "false", "no", "off":
		return false, nil
	case "1", "true", "yes", "on", "require":
		return true, nil
	default:
		return false, errors.New("DATABASE_SSL must be a boolean value")
	}
}
