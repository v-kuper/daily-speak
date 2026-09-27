package app

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/ai"
	"daily-speaking-practice/backend/internal/auth"
	"daily-speaking-practice/backend/internal/db"
	"daily-speaking-practice/backend/internal/feed"
	"daily-speaking-practice/backend/internal/guestpreview"
	"daily-speaking-practice/backend/internal/httpapi"
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
	"daily-speaking-practice/backend/internal/workqueue"
	"github.com/google/uuid"
)

type APIConfig struct {
	DB                 *db.DB
	AIClient           ai.ChatClient
	PracticeGenerator  practice.Generator
	RecordingAnalyzer  recording.Analyzer
	RecordingRewriter  recording.Rewriter
	BrowserCookie      auth.CookieConfig
	IdentityTokens     auth.TokenConfig
	CORS               httpapi.CORSConfig
	MediaStore         storage.Store
	MediaBucket        string
	MediaSigningSecret []byte
	MediaPartSize      int64
	MediaPresignTTL    time.Duration
	LegacyUploads      storage.LegacyUploadStore
	UploadsDir         string
	Operations         operations.Config
}

func NewAPI(config APIConfig) *httpapi.Server {
	if config.BrowserCookie.SameSite == 0 {
		config.BrowserCookie.SameSite = http.SameSiteLaxMode
	}
	uploadsDir := resolveUploadsDir(config.UploadsDir)
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
	mediaStore := config.MediaStore
	if mediaStore == nil && config.DB != nil {
		mediaStore, _ = storage.NewLocal(uploadsDir)
	}
	mediaBucket := strings.TrimSpace(config.MediaBucket)
	if mediaBucket == "" && mediaStore != nil && mediaStore.Backend() == storage.BackendS3 {
		mediaBucket = strings.TrimSpace(os.Getenv("MEDIA_S3_BUCKET"))
	}
	var mediaService *media.Service
	if config.DB != nil && mediaStore != nil {
		mediaService = media.NewService(media.NewSQLRepository(config.DB), mediaStore, media.Config{
			Bucket: mediaBucket, PartSizeBytes: config.MediaPartSize, SignedRequestTTL: config.MediaPresignTTL,
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
	legacyUploads := config.LegacyUploads
	if legacyUploads == nil {
		legacyUploads = storage.NewLegacyUploads(uploadsDir)
	}
	recordingRecords := recording.NewSQLQueryRepository(config.DB)
	recordingDeletion := recording.NewSQLDeletionRepository(config.DB)
	return httpapi.NewServer(httpapi.Dependencies{
		OperationsMonitor: operations.NewMonitor(config.DB, workqueue.NewStore(config.DB)), LegacyUploads: legacyUploads,
		PracticeGenerator:   practiceGenerator,
		FeedService:         feed.NewService(feed.NewSQLRepository(config.DB), feed.NewLocalReplyAudioStore(uploadsDir), uuid.NewString),
		ProfileService:      profile.NewService(profile.NewSQLRepository(config.DB)),
		SubscriptionService: subscription.NewService(subscription.NewSQLRepository(config.DB)),
		RecordingAnalyzer:   recordingAnalyzer, RecordingRewriter: recordingRewriter,
		RecordingCreator: recording.NewCreator(recording.NewSQLCreateUnitOfWork(config.DB)),
		RecordingDeleter: recording.NewDeleter(recordingDeletion, legacyUploads, recordingDeletion, uuid.NewString),
		RecordingReader:  recording.NewReader(recordingRecords),
		RecordingRetryService: recording.NewRetryService(
			recordingRecords, recording.NewSQLRetryUnitOfWork(config.DB), uuid.NewString,
		),
		GuestPreviewStore: guestpreview.NewStore(config.DB, guestpreview.QueueCapacityFromEnv()),
		ShadowingStore:    shadowing.NewStore(config.DB),
		BrowserCookie:     config.BrowserCookie, IdentityTokens: config.IdentityTokens,
		IdentityService: auth.NewIdentityService(config.DB, config.IdentityTokens), CORS: config.CORS,
		MediaService: mediaService, MediaSigner: mediaSigner,
		Operations: config.Operations, Limiter: operations.NewLimiter(config.DB),
		Network: operations.NewNetwork(config.Operations.TrustedProxies), Metrics: operations.NewMetrics(),
	})
}

func resolveUploadsDir(configured string) string {
	if value := strings.TrimSpace(configured); value != "" {
		return value
	}
	if value := strings.TrimSpace(os.Getenv("UPLOADS_DIR")); value != "" {
		return value
	}
	return filepath.Join("public", "uploads")
}
