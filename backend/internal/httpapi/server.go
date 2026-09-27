package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	apidocs "daily-speaking-practice/backend/docs"
	"daily-speaking-practice/backend/internal/auth"
	"daily-speaking-practice/backend/internal/feed"
	"daily-speaking-practice/backend/internal/guestpreview"
	"daily-speaking-practice/backend/internal/logging"
	"daily-speaking-practice/backend/internal/media"
	"daily-speaking-practice/backend/internal/operations"
	"daily-speaking-practice/backend/internal/practice"
	"daily-speaking-practice/backend/internal/profile"
	"daily-speaking-practice/backend/internal/recording"
	"daily-speaking-practice/backend/internal/shadowing"
	"daily-speaking-practice/backend/internal/storage"
	"daily-speaking-practice/backend/internal/subscription"
)

type Dependencies struct {
	OperationsMonitor     *operations.Monitor
	LegacyUploads         storage.LegacyUploadStore
	PracticeGenerator     practice.Generator
	FeedService           *feed.Service
	ProfileService        *profile.Service
	SubscriptionService   *subscription.Service
	RecordingAnalyzer     recording.Analyzer
	RecordingRewriter     recording.Rewriter
	RecordingCreator      *recording.Creator
	RecordingDeleter      *recording.Deleter
	RecordingReader       *recording.Reader
	RecordingRetryService *recording.RetryService
	GuestPreviewStore     *guestpreview.Store
	ShadowingStore        *shadowing.Store
	BrowserCookie         auth.CookieConfig
	IdentityTokens        auth.TokenConfig
	IdentityService       *auth.IdentityService
	CORS                  CORSConfig
	MediaService          *media.Service
	MediaSigner           *media.URLSigner
	Operations            operations.Config
	Limiter               requestLimiter
	Network               operations.Network
	Metrics               *operations.Metrics
}

type Server struct {
	operationsMonitor     *operations.Monitor
	legacyUploads         storage.LegacyUploadStore
	practiceGenerator     practice.Generator
	feedService           *feed.Service
	profileService        *profile.Service
	subscriptionService   *subscription.Service
	recordingAnalyzer     recording.Analyzer
	recordingRewriter     recording.Rewriter
	recordingCreator      *recording.Creator
	recordingDeleter      *recording.Deleter
	recordingReader       *recording.Reader
	recordingRetryService *recording.RetryService
	guestPreviewStore     *guestpreview.Store
	shadowingStore        *shadowing.Store
	browserCookie         auth.CookieConfig
	identityTokens        auth.TokenConfig
	identityService       *auth.IdentityService
	cors                  CORSConfig
	mediaService          *media.Service
	mediaSigner           *media.URLSigner
	operations            operations.Config
	limiter               requestLimiter
	network               operations.Network
	metrics               *operations.Metrics
}

type requestLimiter interface {
	Allow(context.Context, string, string, operations.Limit) (operations.Decision, error)
}

func NewServer(dependencies Dependencies) *Server {
	return &Server{
		operationsMonitor: dependencies.OperationsMonitor,
		legacyUploads:     dependencies.LegacyUploads,
		practiceGenerator: dependencies.PracticeGenerator, feedService: dependencies.FeedService,
		profileService: dependencies.ProfileService, subscriptionService: dependencies.SubscriptionService,
		recordingAnalyzer: dependencies.RecordingAnalyzer, recordingRewriter: dependencies.RecordingRewriter,
		recordingCreator: dependencies.RecordingCreator, recordingDeleter: dependencies.RecordingDeleter,
		recordingReader: dependencies.RecordingReader, recordingRetryService: dependencies.RecordingRetryService,
		guestPreviewStore: dependencies.GuestPreviewStore, shadowingStore: dependencies.ShadowingStore,
		browserCookie: dependencies.BrowserCookie, identityTokens: dependencies.IdentityTokens,
		identityService: dependencies.IdentityService, cors: dependencies.CORS,
		mediaService: dependencies.MediaService, mediaSigner: dependencies.MediaSigner,
		operations: dependencies.Operations, limiter: dependencies.Limiter,
		network: dependencies.Network, metrics: dependencies.Metrics,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(apidocs.OpenAPIJSON)
	})
	mux.HandleFunc("/docs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(apidocs.SwaggerHTML)
	})
	mux.HandleFunc("/healthz", s.healthz)
	mux.HandleFunc("/readyz", s.readyz)
	mux.HandleFunc("/metrics", s.handleMetrics)
	mux.HandleFunc("/api/v1", s.routeV1)
	mux.HandleFunc("/api/v1/media/uploads", s.routeV1)
	mux.HandleFunc("/api/v1/media/uploads/", s.routeMediaUploadEntry)
	mux.HandleFunc("/api/v1/media/local/", s.routeSignedLocalMedia)
	mux.HandleFunc("/api/v1/", s.routeV1)
	mux.HandleFunc("/api/", s.routeAPI)
	mux.HandleFunc("/uploads/shadowing", s.handleShadowingUpload)
	mux.HandleFunc("/uploads/shadowing/", s.handleShadowingUpload)
	mux.Handle(uploadsURLPrefix, http.HandlerFunc(s.handleLegacyUpload))
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
	})
	corsHandler := s.cors.Wrap(s.withRateLimit(mux))
	root := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if (r.URL.Path == "/openapi.json" || r.URL.Path == "/docs") && r.Method != http.MethodGet {
			mux.ServeHTTP(w, r)
			return
		}
		corsHandler.ServeHTTP(w, r)
	})
	return withRequestID(withTraceContext(s.withSecurityHeaders(s.withRequestMetrics(root))))
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) routeAPI(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSuffix(r.URL.Path, "/")
	switch {
	case path == "/api/daily-questions" && r.Method == http.MethodGet:
		s.handleDailyQuestions(w, r)
	case path == "/api/topic-guidance" && r.Method == http.MethodGet:
		s.handleTopicGuidance(w, r)
	case path == "/api/study-words" && r.Method == http.MethodGet:
		s.handleStudyWords(w, r)
	case path == "/api/user/data" && r.Method == http.MethodGet:
		s.handleUserData(w, r)
	case path == "/api/user/interests" && r.Method == http.MethodPut:
		s.handleUserInterests(w, r)
	case path == "/api/user/ollama-model" && r.Method == http.MethodGet:
		s.handleUserOllamaModel(w, r)
	case path == "/api/user/subscription" && r.Method == http.MethodGet:
		s.handleGetSubscription(w, r)
	case path == "/api/user/subscription" && r.Method == http.MethodPost:
		s.handleActivateSubscription(w, r)
	case path == "/api/user/subscription" && r.Method == http.MethodDelete:
		s.handleCancelSubscription(w, r)
	case path == "/api/user/english-level" && r.Method == http.MethodGet:
		s.handleGetEnglishLevel(w, r)
	case path == "/api/user/english-level" && r.Method == http.MethodPut:
		s.handlePutEnglishLevel(w, r)
	case strings.HasPrefix(path, "/api/recordings/") && strings.HasSuffix(path, "/retry") && r.Method == http.MethodPost:
		s.routeRecordingRetryPath(w, r, strings.TrimPrefix(path, "/api/recordings/"))
	case strings.HasPrefix(path, "/api/recordings/") && strings.HasSuffix(path, "/shadowing") && r.Method == http.MethodPost:
		s.routeShadowingPath(w, r, strings.TrimPrefix(path, "/api/recordings/"))
	case strings.HasPrefix(path, "/api/recordings/") && r.Method == http.MethodGet:
		s.handleGetRecording(w, r, strings.TrimPrefix(path, "/api/recordings/"))
	case strings.HasPrefix(path, "/api/recordings/") && r.Method == http.MethodDelete:
		s.handleDeleteRecording(w, r, strings.TrimPrefix(path, "/api/recordings/"))
	case path == "/api/feed/posts" && r.Method == http.MethodGet:
		s.handleFeedPosts(w, r)
	case path == "/api/feed/posts" && r.Method == http.MethodPost:
		s.handleCreateFeedPost(w, r)
	case strings.HasPrefix(path, "/api/feed/posts/"):
		s.routeFeedPostPath(w, r, strings.TrimPrefix(path, "/api/feed/posts/"))
	case strings.HasPrefix(path, "/api/feed/replies/"):
		s.routeFeedReplyPath(w, r, strings.TrimPrefix(path, "/api/feed/replies/"))
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
	}
}

func (s *Server) authorizedUser(w http.ResponseWriter, r *http.Request, scope string) (*auth.User, bool) {
	return s.authorize(w, r, scope)
}

func (s *Server) authorize(w http.ResponseWriter, r *http.Request, scope string) (*auth.User, bool) {
	started := time.Now()
	logger := logging.ForRequest(scope, r)
	writeError := func(status int, message string) {
		writeJSON(w, status, map[string]string{"error": message})
	}
	if bearer, present := bearerToken(r); present {
		identity, err := s.identityService.Authenticate(r.Context(), bearer)
		if err != nil {
			logger.Info("request.unauthorized", map[string]any{"status": 401, "durationMs": logging.ElapsedMs(started)})
			writeError(http.StatusUnauthorized, "Unauthorized")
			return nil, false
		}
		if identity.User == nil {
			logger.Info("request.unauthorized", map[string]any{"status": 401, "durationMs": logging.ElapsedMs(started)})
			writeError(http.StatusUnauthorized, "Unauthorized")
			return nil, false
		}
		return identity.User, true
	}
	logger.Info("request.unauthorized", map[string]any{"status": 401, "durationMs": logging.ElapsedMs(started)})
	writeError(http.StatusUnauthorized, "Unauthorized")
	return nil, false
}

func bearerToken(r *http.Request) (string, bool) {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if header == "" {
		return "", false
	}
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
		return "", true
	}
	return parts[1], true
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
