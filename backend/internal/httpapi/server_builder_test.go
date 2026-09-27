package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"daily-speaking-practice/backend/internal/ai"
	"daily-speaking-practice/backend/internal/auth"
	"daily-speaking-practice/backend/internal/background"
	"daily-speaking-practice/backend/internal/db"
	"daily-speaking-practice/backend/internal/feed"
	"daily-speaking-practice/backend/internal/guestpreview"
	"daily-speaking-practice/backend/internal/media"
	"daily-speaking-practice/backend/internal/operations"
	"daily-speaking-practice/backend/internal/practice"
	practiceollama "daily-speaking-practice/backend/internal/practice/ollamaadapter"
	"daily-speaking-practice/backend/internal/profile"
	"daily-speaking-practice/backend/internal/recording"
	recordingollama "daily-speaking-practice/backend/internal/recording/ollamaadapter"
	"daily-speaking-practice/backend/internal/shadowing"
	"daily-speaking-practice/backend/internal/storage"
	"daily-speaking-practice/backend/internal/subscription"
	"daily-speaking-practice/backend/internal/transcription"
	"daily-speaking-practice/backend/internal/tts"
	"daily-speaking-practice/backend/internal/workqueue"
	"github.com/google/uuid"
)

type Config struct {
	DB                       *db.DB
	Synthesizer              tts.Synthesizer
	AIClient                 ai.ChatClient
	PracticeGenerator        practice.Generator
	RecordingAnalyzer        recording.Analyzer
	RecordingRewriter        recording.Rewriter
	RecordingPreviewAnalyzer recording.PreviewAnalyzer
	BrowserCookie            auth.CookieConfig
	IdentityTokens           auth.TokenConfig
	CORS                     CORSConfig
	MediaStore               storage.Store
	MediaBucket              string
	MediaSigningSecret       []byte
	MediaPartSize            int64
	MediaPresignTTL          time.Duration
	LegacyUploads            storage.LegacyUploadStore
	TranscribeAudio          func(context.Context, string) (string, error)
	ProbeAudioDuration       func(context.Context, string) (time.Duration, error)
	Operations               operations.Config
}

var testBackgroundRuntimes sync.Map
var testJobStores sync.Map

func resolveUploadsDir() string {
	if value := strings.TrimSpace(os.Getenv("UPLOADS_DIR")); value != "" {
		return value
	}
	return filepath.Join("public", "uploads")
}

func testBackgroundRuntime(t *testing.T, server *Server) *background.Runtime {
	t.Helper()
	value, ok := testBackgroundRuntimes.Load(server)
	if !ok {
		t.Fatal("test background runtime is not configured")
	}
	return value.(*background.Runtime)
}

func testJobStore(t *testing.T, server *Server) *workqueue.Store {
	t.Helper()
	value, ok := testJobStores.Load(server)
	if !ok {
		t.Fatal("test job store is not configured")
	}
	return value.(*workqueue.Store)
}

func newTestServer(config Config) *Server {
	if config.BrowserCookie.SameSite == 0 {
		config.BrowserCookie.SameSite = http.SameSiteLaxMode
	}
	synthesizer := config.Synthesizer
	if synthesizer == nil {
		synthesizer = tts.NewCartesia(tts.ConfigFromEnv())
	}
	aiClient := config.AIClient
	if aiClient == nil {
		aiClient = ai.OllamaClient{}
	}
	practiceGenerator := config.PracticeGenerator
	if practiceGenerator == nil {
		practiceGenerator = practice.NewService(practiceollama.New(aiClient))
	}
	recordingService := recording.NewAnalysisService(recordingollama.New(aiClient), recording.AnalysisConfigFromEnv())
	recordingAnalyzer := config.RecordingAnalyzer
	if recordingAnalyzer == nil {
		recordingAnalyzer = recordingService
	}
	recordingRewriter := config.RecordingRewriter
	if recordingRewriter == nil {
		recordingRewriter = recordingService
	}
	previewAnalyzer := config.RecordingPreviewAnalyzer
	if previewAnalyzer == nil {
		previewAnalyzer = recordingService
	}
	mediaStore := config.MediaStore
	if mediaStore == nil && config.DB != nil {
		mediaStore, _ = storage.NewLocal(resolveUploadsDir())
	}
	legacyUploads := config.LegacyUploads
	if legacyUploads == nil {
		legacyUploads = storage.NewLegacyUploads(resolveUploadsDir())
	}
	var mediaService *media.Service
	if config.DB != nil && mediaStore != nil {
		mediaService = media.NewService(media.NewSQLRepository(config.DB), mediaStore, media.Config{
			Bucket: config.MediaBucket, PartSizeBytes: config.MediaPartSize, SignedRequestTTL: config.MediaPresignTTL,
		})
	}
	signingSecret := config.MediaSigningSecret
	if len(signingSecret) == 0 {
		signingSecret = []byte(strings.TrimSpace(os.Getenv("MEDIA_URL_SIGNING_SECRET")))
	}
	if len(signingSecret) == 0 {
		signingSecret = []byte(strings.TrimSpace(os.Getenv("AUTH_ACCESS_TOKEN_SECRET")))
	}
	mediaSigner, _ := media.NewURLSigner(signingSecret)
	recordingRecords := recording.NewSQLQueryRepository(config.DB)
	recordingDeletion := recording.NewSQLDeletionRepository(config.DB)
	jobStore := workqueue.NewStore(config.DB)
	guestStore := guestpreview.NewStore(config.DB, guestpreview.QueueCapacityFromEnv())
	shadowingStore := shadowing.NewStore(config.DB)
	server := NewServer(Dependencies{
		OperationsMonitor: operations.NewMonitor(config.DB, jobStore), LegacyUploads: legacyUploads,
		PracticeGenerator:   practiceGenerator,
		FeedService:         feed.NewService(feed.NewSQLRepository(config.DB), feed.NewLocalReplyAudioStore(resolveUploadsDir()), uuid.NewString),
		ProfileService:      profile.NewService(profile.NewSQLRepository(config.DB)),
		SubscriptionService: subscription.NewService(subscription.NewSQLRepository(config.DB)),
		RecordingAnalyzer:   recordingAnalyzer, RecordingRewriter: recordingRewriter,
		RecordingCreator: recording.NewCreator(recording.NewSQLCreateUnitOfWork(config.DB)),
		RecordingDeleter: recording.NewDeleter(recordingDeletion, legacyUploads, recordingDeletion, uuid.NewString),
		RecordingReader:  recording.NewReader(recordingRecords),
		RecordingRetryService: recording.NewRetryService(
			recordingRecords, recording.NewSQLRetryUnitOfWork(config.DB), legacyUploads, uuid.NewString,
		),
		GuestPreviewStore: guestStore, ShadowingStore: shadowingStore,
		BrowserCookie: config.BrowserCookie, IdentityTokens: config.IdentityTokens,
		IdentityService: auth.NewIdentityService(config.DB, config.IdentityTokens), CORS: config.CORS,
		MediaService: mediaService, MediaSigner: mediaSigner,
		Operations: config.Operations, Limiter: operations.NewLimiter(config.DB),
		Network: operations.NewNetwork(config.Operations.TrustedProxies), Metrics: operations.NewMetrics(),
	})
	if config.DB == nil || mediaStore == nil {
		return server
	}
	transcribe := config.TranscribeAudio
	if transcribe == nil {
		transcribe = transcription.TranscribeAudioWithLocalWhisper
	}
	probe := config.ProbeAudioDuration
	if probe == nil {
		probe = media.ProbeAudioDuration
	}
	transcribeForProcessing := func(ctx context.Context, path string) (string, error) {
		transcript, err := transcribe(ctx, path)
		var typed transcription.Error
		if errors.As(err, &typed) {
			return "", errors.New(typed.Message)
		}
		return transcript, err
	}
	materializer := media.NewMaterializer(config.DB, mediaStore)
	recordingRepository := recording.NewSQLProcessingRepository(config.DB)
	runtime := background.NewRuntime(background.Dependencies{
		DB: config.DB, JobStore: jobStore,
		RecordingProcessor: recording.NewProcessor(recording.ProcessingDependencies{
			Repository: recordingRepository, Materializer: materializer, ResolveLegacyAudio: legacyUploads.Path,
			ProbeAudioDuration: probe, Transcribe: transcribeForProcessing, Analyzer: recordingAnalyzer,
			Rewriter: recordingRewriter, NewID: uuid.NewString,
		}),
		RecordingRepository: recordingRepository,
		GuestPreviewProcessor: guestpreview.NewProcessor(guestpreview.ProcessorDependencies{
			Store: guestStore, Materializer: materializer, ProbeAudioDuration: probe,
			Transcribe: transcribeForProcessing, Analyzer: previewAnalyzer,
		}),
		GuestPreviewStore: guestStore,
		ShadowingProcessor: shadowing.NewProcessor(shadowing.ProcessorDependencies{
			Store: shadowingStore, Synthesizer: synthesizer, MediaStore: mediaStore,
			MediaBucket: config.MediaBucket, LocalSaver: shadowing.NewLocalSaver(resolveUploadsDir()), NewID: uuid.NewString,
		}),
		ShadowingStore: shadowingStore,
		MediaCleanup:   media.NewCleanup(config.DB, mediaService, mediaStore, legacyUploads),
	})
	testBackgroundRuntimes.Store(server, runtime)
	testJobStores.Store(server, jobStore)
	return server
}

type countingGuestPreviewAI struct {
	calls    int
	response string
	body     string
}

func (client *countingGuestPreviewAI) PostChat(_ context.Context, body any) (ai.ChatResponse, error) {
	client.calls++
	encoded, _ := json.Marshal(body)
	client.body = strings.ReplaceAll(strings.TrimSpace(string(encoded)), "\\u003c", "<")
	return ai.ChatResponse{Response: client.response}, nil
}
