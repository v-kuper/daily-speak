package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	apidocs "daily-speaking-practice/backend/docs"
	"daily-speaking-practice/backend/internal/auth"
	"daily-speaking-practice/backend/internal/guestpreview"
	"daily-speaking-practice/backend/internal/interview"
	"daily-speaking-practice/backend/internal/media"
	"daily-speaking-practice/backend/internal/operations"
	"daily-speaking-practice/backend/internal/practice"
	"daily-speaking-practice/backend/internal/profile"
	"daily-speaking-practice/backend/internal/recording"
	"daily-speaking-practice/backend/internal/shadowing"
	"daily-speaking-practice/backend/internal/subscription"
)

type Dependencies struct {
	OperationsMonitor         *operations.Monitor
	PracticeGenerator         practice.Generator
	ProfileService            *profile.Service
	SubscriptionService       *subscription.Service
	RecordingCreator          *recording.Creator
	RecordingDeleter          *recording.Deleter
	RecordingReader           *recording.Reader
	RecordingRetryService     *recording.RetryService
	RecordingStrengthsService *recording.StrengthsService
	GuestPreviewStore         *guestpreview.Store
	InterviewService          *interview.Service
	QuestionAudioService      *interview.QuestionAudioService
	AnswerAttemptService      *interview.AnswerAttemptService
	FeedbackAudioService      *recording.FeedbackAudioService
	FeedbackReanalysisService *recording.FeedbackReanalysisService
	ShadowingStore            *shadowing.Store
	BrowserCookie             auth.CookieConfig
	IdentityTokens            auth.TokenConfig
	IdentityService           *auth.IdentityService
	CORS                      CORSConfig
	MediaService              *media.Service
	MediaSigner               *media.URLSigner
	Operations                operations.Config
	Limiter                   requestLimiter
	Network                   operations.Network
	Metrics                   *operations.Metrics
}

type Server struct {
	operationsMonitor         *operations.Monitor
	practiceGenerator         practice.Generator
	profileService            *profile.Service
	subscriptionService       *subscription.Service
	recordingCreator          *recording.Creator
	recordingDeleter          *recording.Deleter
	recordingReader           *recording.Reader
	recordingRetryService     *recording.RetryService
	recordingStrengthsService *recording.StrengthsService
	guestPreviewStore         *guestpreview.Store
	interviewService          *interview.Service
	questionAudioService      *interview.QuestionAudioService
	answerAttemptService      *interview.AnswerAttemptService
	feedbackAudioService      *recording.FeedbackAudioService
	feedbackReanalysisService *recording.FeedbackReanalysisService
	shadowingStore            *shadowing.Store
	browserCookie             auth.CookieConfig
	identityTokens            auth.TokenConfig
	identityService           *auth.IdentityService
	cors                      CORSConfig
	mediaService              *media.Service
	mediaSigner               *media.URLSigner
	operations                operations.Config
	limiter                   requestLimiter
	network                   operations.Network
	metrics                   *operations.Metrics
}

type requestLimiter interface {
	Allow(context.Context, string, string, operations.Limit) (operations.Decision, error)
}

func NewServer(dependencies Dependencies) *Server {
	return &Server{
		operationsMonitor: dependencies.OperationsMonitor,
		practiceGenerator: dependencies.PracticeGenerator,
		profileService:    dependencies.ProfileService, subscriptionService: dependencies.SubscriptionService,
		recordingCreator: dependencies.RecordingCreator, recordingDeleter: dependencies.RecordingDeleter,
		recordingReader: dependencies.RecordingReader, recordingRetryService: dependencies.RecordingRetryService,
		recordingStrengthsService: dependencies.RecordingStrengthsService,
		guestPreviewStore:         dependencies.GuestPreviewStore, shadowingStore: dependencies.ShadowingStore,
		interviewService:     dependencies.InterviewService,
		questionAudioService: dependencies.QuestionAudioService, answerAttemptService: dependencies.AnswerAttemptService,
		feedbackAudioService: dependencies.FeedbackAudioService, feedbackReanalysisService: dependencies.FeedbackReanalysisService,
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
