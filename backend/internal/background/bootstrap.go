package background

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/ai"
	"daily-speaking-practice/backend/internal/db"
	"daily-speaking-practice/backend/internal/guestpreview"
	"daily-speaking-practice/backend/internal/media"
	"daily-speaking-practice/backend/internal/recording"
	recordingollama "daily-speaking-practice/backend/internal/recording/ollamaadapter"
	"daily-speaking-practice/backend/internal/shadowing"
	"daily-speaking-practice/backend/internal/storage"
	"daily-speaking-practice/backend/internal/transcription"
	"daily-speaking-practice/backend/internal/tts"
	"daily-speaking-practice/backend/internal/workqueue"
	"github.com/google/uuid"
)

type Config struct {
	DB                 *db.DB
	MediaStore         storage.Store
	MediaBucket        string
	MediaPartSize      int64
	MediaPresignTTL    time.Duration
	UploadsDir         string
	AIClient           ai.ChatClient
	Synthesizer        shadowing.Synthesizer
	TranscribeAudio    func(context.Context, string) (string, error)
	ProbeAudioDuration func(context.Context, string) (time.Duration, error)
}

func New(config Config) *Runtime {
	uploadsDir := strings.TrimSpace(config.UploadsDir)
	if uploadsDir == "" {
		uploadsDir = filepath.Join("public", "uploads")
	}
	aiClient := config.AIClient
	if aiClient == nil {
		aiClient = ai.OllamaClient{}
	}
	analysis := recording.NewAnalysisService(recordingollama.New(aiClient), recording.AnalysisConfigFromEnv())
	transcribe := config.TranscribeAudio
	if transcribe == nil {
		transcribe = transcription.TranscribeAudioWithLocalWhisper
	}
	transcribeForProcessing := func(ctx context.Context, path string) (string, error) {
		transcript, err := transcribe(ctx, path)
		if err != nil {
			var typed transcription.Error
			if errors.As(err, &typed) {
				return "", errors.New(typed.Message)
			}
		}
		return transcript, err
	}
	probe := config.ProbeAudioDuration
	if probe == nil {
		probe = media.ProbeAudioDuration
	}
	synthesizer := config.Synthesizer
	if synthesizer == nil {
		synthesizer = tts.NewCartesia(tts.ConfigFromEnv())
	}
	mediaService := media.NewService(media.NewSQLRepository(config.DB), config.MediaStore, media.Config{
		Bucket: config.MediaBucket, PartSizeBytes: config.MediaPartSize, SignedRequestTTL: config.MediaPresignTTL,
	})
	materializer := media.NewMaterializer(config.DB, config.MediaStore)
	legacyUploads := storage.NewLegacyUploads(uploadsDir)
	recordingRepository := recording.NewSQLProcessingRepository(config.DB)
	recordingProcessor := recording.NewProcessor(recording.ProcessingDependencies{
		Repository: recordingRepository, Materializer: materializer,
		ResolveLegacyAudio: legacyUploads.Path, ProbeAudioDuration: probe,
		Transcribe: transcribeForProcessing, Analyzer: analysis, Rewriter: analysis, NewID: uuid.NewString,
	})
	guestStore := guestpreview.NewStore(config.DB, guestpreview.QueueCapacityFromEnv())
	guestProcessor := guestpreview.NewProcessor(guestpreview.ProcessorDependencies{
		Store: guestStore, Materializer: materializer, ProbeAudioDuration: probe,
		Transcribe: transcribeForProcessing, Analyzer: analysis,
	})
	shadowStore := shadowing.NewStore(config.DB)
	shadowProcessor := shadowing.NewProcessor(shadowing.ProcessorDependencies{
		Store: shadowStore, Synthesizer: synthesizer, MediaStore: config.MediaStore,
		MediaBucket: config.MediaBucket, LocalSaver: shadowing.NewLocalSaver(uploadsDir), NewID: uuid.NewString,
	})
	cleanup := media.NewCleanup(config.DB, mediaService, config.MediaStore, legacyUploads.Remove)
	return NewRuntime(Dependencies{
		DB: config.DB, JobStore: workqueue.NewStore(config.DB),
		RecordingProcessor: recordingProcessor, RecordingRepository: recordingRepository,
		GuestPreviewProcessor: guestProcessor, GuestPreviewStore: guestStore,
		ShadowingProcessor: shadowProcessor, ShadowingStore: shadowStore, MediaCleanup: cleanup,
	})
}
