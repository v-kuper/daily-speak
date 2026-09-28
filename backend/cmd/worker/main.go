package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"daily-speaking-practice/backend/internal/app"
	"daily-speaking-practice/backend/internal/db"
	"daily-speaking-practice/backend/internal/storage"
	"daily-speaking-practice/backend/internal/transcription"
	"daily-speaking-practice/backend/internal/worker"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if len(os.Args) == 2 && os.Args[1] == "--check-groq" {
		checkGroq(ctx)
		return
	}
	if strings.TrimSpace(os.Getenv("GROQ_API_KEY")) == "" {
		log.Fatal("GROQ_API_KEY is required for worker transcription")
	}

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

	config, err := worker.ConfigFromEnv()
	if err != nil {
		log.Fatalf("worker configuration failed: %v", err)
	}
	runtime := app.NewWorker(app.WorkerConfig{
		DB: database, MediaStore: mediaStore, MediaBucket: mediaConfig.S3Bucket,
		MediaPartSize: mediaConfig.MultipartPartSize, MediaPresignTTL: mediaConfig.PresignTTL,
	})
	log.Printf("daily-speaking worker started")
	if err := runtime.Run(ctx, config); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("worker failed: %v", err)
	}
}

func checkGroq(ctx context.Context) {
	ffmpeg := strings.TrimSpace(os.Getenv("FFMPEG_BINARY_PATH"))
	if ffmpeg == "" {
		ffmpeg = "ffmpeg"
	}
	if _, err := exec.LookPath(ffmpeg); err != nil {
		log.Fatal("ffmpeg is unavailable for Groq transcription")
	}
	groq := transcription.NewGroq(transcription.GroqConfig{
		APIKey: os.Getenv("GROQ_API_KEY"),
		Model:  os.Getenv("GROQ_WHISPER_MODEL"),
	})
	if err := groq.Check(ctx); err != nil {
		log.Fatalf("Groq configuration check failed: %v", err)
	}
	log.Println("groq-config-ok")
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
