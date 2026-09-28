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
	DB                   *db.DB
	MediaStore           storage.Store
	MediaBucket          string
	MediaPartSize        int64
	MediaPresignTTL      time.Duration
	AIClient             ai.ChatClient
	Synthesizer          shadowing.Synthesizer
	TranscribeAudio      func(context.Context, string) (string, error)
	TranscribeTimedAudio func(context.Context, string) (recording.TimedTranscript, error)
	ProbeAudioDuration   func(context.Context, string) (time.Duration, error)
}

func NewWorker(config WorkerConfig) *background.Runtime {
	aiClient := config.AIClient
	if aiClient == nil {
		aiClient = ai.OllamaClient{}
	}
	analysis := recording.NewAnalysisService(recordingollama.New(aiClient), recording.AnalysisConfigFromEnv())
	groq := transcription.NewGroq(transcription.GroqConfig{
		APIKey: os.Getenv("GROQ_API_KEY"), Model: os.Getenv("GROQ_WHISPER_MODEL"),
		Language: os.Getenv("TRANSCRIPTION_LANGUAGE"), Prompt: os.Getenv("TRANSCRIPTION_PROMPT"),
		FFmpegPath: os.Getenv("FFMPEG_BINARY_PATH"),
	})
	transcribe := config.TranscribeAudio
	if transcribe == nil {
		transcribe = groq.Transcribe
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
	timedTranscribe := config.TranscribeTimedAudio
	if timedTranscribe == nil && config.TranscribeAudio == nil {
		timedTranscribe = func(ctx context.Context, path string) (recording.TimedTranscript, error) {
			result, err := groq.TranscribeTimed(ctx, path)
			if err != nil {
				return recording.TimedTranscript{}, err
			}
			segments := make([]recording.TimedSegment, 0, len(result.Segments))
			for _, segment := range result.Segments {
				segments = append(segments, recording.TimedSegment{
					StartMS: segment.StartMS, EndMS: segment.EndMS, Text: segment.Text,
				})
			}
			return recording.TimedTranscript{Text: result.Text, Segments: segments}, nil
		}
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
	recordingRepository := recording.NewSQLProcessingRepository(config.DB)
	recordingProcessor := recording.NewProcessor(recording.ProcessingDependencies{
		Repository: recordingRepository, Materializer: materializer,
		ProbeAudioDuration: probe,
		Transcribe:         transcribeForProcessing, TranscribeTimed: timedTranscribe,
		Analyzer: analysis, Rewriter: analysis, NewID: uuid.NewString,
	})
	guestStore := guestpreview.NewStore(config.DB, guestpreview.QueueCapacityFromEnv())
	guestProcessor := guestpreview.NewProcessor(guestpreview.ProcessorDependencies{
		Store: guestStore, Materializer: materializer, ProbeAudioDuration: probe,
		Transcribe: transcribeForProcessing, TranscribeTimed: timedTranscribe, Analyzer: analysis,
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
		GuestPreviewProcessor: guestProcessor, GuestPreviewStore: guestStore,
		InterviewProcessor: interviewProcessor, InterviewStore: interviewRepository,
		ShadowingProcessor: shadowProcessor, ShadowingStore: shadowStore, MediaCleanup: cleanup,
	})
}
