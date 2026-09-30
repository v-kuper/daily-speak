package app

import (
	"context"
	"errors"
	"os"
	"time"

	"daily-speaking-practice/backend/internal/ai"
	"daily-speaking-practice/backend/internal/background"
	"daily-speaking-practice/backend/internal/db"
	"daily-speaking-practice/backend/internal/guestpreview"
	"daily-speaking-practice/backend/internal/interview"
	interviewollama "daily-speaking-practice/backend/internal/interview/ollamaadapter"
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

type WorkerConfig struct {
	DB                 *db.DB
	MediaStore         storage.Store
	MediaBucket        string
	MediaPartSize      int64
	MediaPresignTTL    time.Duration
	AIClient           ai.ChatClient
	Synthesizer        shadowing.Synthesizer
	TranscribeAudio    func(context.Context, string) (string, error)
	ProbeAudioDuration func(context.Context, string) (time.Duration, error)
}

func NewWorker(config WorkerConfig) *background.Runtime {
	aiClient := config.AIClient
	if aiClient == nil {
		aiClient = ai.OllamaClient{}
	}
	analysis := recording.NewAnalysisService(recordingollama.New(aiClient), recordingAnalysisConfig())
	cartesia := transcription.NewCartesia(transcription.CartesiaConfig{
		APIKey: os.Getenv("CARTESIA_API_KEY"), Language: os.Getenv("TRANSCRIPTION_LANGUAGE"),
		APIVersion: os.Getenv("CARTESIA_API_VERSION"),
	})
	transcribe := config.TranscribeAudio
	if transcribe == nil {
		transcribe = cartesia.Transcribe
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
		synthesizer = tts.NewCartesia(tts.Config{
			APIKey:     os.Getenv("CARTESIA_API_KEY"),
			VoiceID:    os.Getenv("CARTESIA_VOICE_ID"),
			Model:      os.Getenv("CARTESIA_MODEL"),
			APIURL:     os.Getenv("CARTESIA_API_URL"),
			APIVersion: os.Getenv("CARTESIA_API_VERSION"),
		})
	}
	mediaService := media.NewService(media.NewSQLRepository(config.DB), config.MediaStore, media.Config{
		Bucket: config.MediaBucket, PartSizeBytes: config.MediaPartSize, SignedRequestTTL: config.MediaPresignTTL,
	})
	materializer := media.NewMaterializer(config.DB, config.MediaStore)
	recordingRepository := recording.NewSQLProcessingRepository(config.DB)
	recordingProcessor := recording.NewProcessor(recording.ProcessingDependencies{
		Repository: recordingRepository, Materializer: materializer,
		ProbeAudioDuration: probe,
		Transcribe:         transcribeForProcessing,
		Analyzer:           analysis, Rewriter: analysis, NewID: uuid.NewString,
	})
	guestStore := guestpreview.NewStore(config.DB, guestpreview.QueueCapacityFromEnv())
	guestProcessor := guestpreview.NewProcessor(guestpreview.ProcessorDependencies{
		Store: guestStore, Materializer: materializer, ProbeAudioDuration: probe,
		Transcribe: transcribeForProcessing, Analyzer: analysis,
	})
	interviewRepository := interview.NewSQLRepository(config.DB)
	interviewProcessor := interview.NewProcessor(interviewRepository, materializer,
		interview.TranscribeFunc(transcribeForProcessing),
		interview.NewLocalGenerator(interviewollama.New(aiClient)))
	shadowStore := shadowing.NewStore(config.DB)
	shadowProcessor := shadowing.NewProcessor(shadowing.ProcessorDependencies{
		Store: shadowStore, Synthesizer: synthesizer, MediaStore: config.MediaStore,
		MediaBucket: config.MediaBucket, NewID: uuid.NewString,
	})
	cleanup := media.NewCleanup(config.DB, mediaService, config.MediaStore)
	return background.NewRuntime(background.Dependencies{
		DB: config.DB, JobStore: workqueue.NewStore(config.DB),
		RecordingProcessor: recordingProcessor, RecordingRepository: recordingRepository,
		StrengthsProcessor: recording.NewStrengthsService(recordingRepository, recordingRepository, analysis, uuid.NewString), StrengthsRepository: recordingRepository,
		GuestPreviewProcessor: guestProcessor, GuestPreviewStore: guestStore,
		InterviewProcessor: interviewProcessor, InterviewStore: interviewRepository,
		ShadowingProcessor: shadowProcessor, ShadowingStore: shadowStore, MediaCleanup: cleanup,
	})
}
