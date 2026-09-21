package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"daily-speaking-practice/backend/internal/ai"
	"daily-speaking-practice/backend/internal/auth"
	"daily-speaking-practice/backend/internal/db"
	"daily-speaking-practice/backend/internal/logging"
	"daily-speaking-practice/backend/internal/tts"
)

type Config struct {
	DB            *db.DB
	Synthesizer   tts.Synthesizer
	AIClient      ai.ChatClient
	SessionCookie auth.CookieConfig
	CORS          CORSConfig
}

type shadowingJob struct {
	id     string
	cancel context.CancelFunc
}

type recordingProcessingJob struct {
	id     string
	cancel context.CancelFunc
}

type Server struct {
	db                      *db.DB
	recordingProcessingMu   sync.Mutex
	recordingProcessingJobs map[string]recordingProcessingJob
	fileDeletionWorkerOnce  sync.Once
	fileDeletionWake        chan struct{}
	removeStoredUploads     func([]string) error
	synthesizer             tts.Synthesizer
	shadowingProcessingMu   sync.Mutex
	shadowingProcessingJobs map[string]shadowingJob
	aiClient                ai.ChatClient
	sessionCookie           auth.CookieConfig
	cors                    CORSConfig
}

func NewServer(config Config) *Server {
	if config.SessionCookie.SameSite == 0 {
		config.SessionCookie.SameSite = http.SameSiteLaxMode
	}
	synthesizer := config.Synthesizer
	if synthesizer == nil {
		synthesizer = tts.NewCartesia(tts.ConfigFromEnv())
	}
	aiClient := config.AIClient
	if aiClient == nil {
		aiClient = ai.OllamaClient{}
	}
	return &Server{
		db:                      config.DB,
		recordingProcessingJobs: map[string]recordingProcessingJob{},
		fileDeletionWake:        make(chan struct{}, 1),
		removeStoredUploads:     removeStoredUploadFiles,
		synthesizer:             synthesizer,
		shadowingProcessingJobs: map[string]shadowingJob{},
		aiClient:                aiClient,
		sessionCookie:           config.SessionCookie,
		cors:                    config.CORS,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.healthz)
	mux.HandleFunc("/api/", s.routeAPI)
	mux.HandleFunc("/uploads/shadowing", s.handleShadowingUpload)
	mux.HandleFunc("/uploads/shadowing/", s.handleShadowingUpload)
	mux.Handle(uploadsURLPrefix, uploadsHandler())
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
	})
	return s.cors.Wrap(mux)
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) routeAPI(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSuffix(r.URL.Path, "/")
	switch {
	case path == "/api/auth/register" && r.Method == http.MethodPost:
		s.handleRegister(w, r)
	case path == "/api/auth/login" && r.Method == http.MethodPost:
		s.handleLogin(w, r)
	case path == "/api/auth/session" && r.Method == http.MethodGet:
		s.handleSession(w, r)
	case path == "/api/auth/logout" && r.Method == http.MethodPost:
		s.handleLogout(w, r)
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
	case path == "/api/user/recordings" && r.Method == http.MethodPost:
		s.handleCreateRecording(w, r)
	case strings.HasPrefix(path, "/api/recordings/") && strings.HasSuffix(path, "/retry") && r.Method == http.MethodPost:
		s.routeRecordingRetryPath(w, r, strings.TrimPrefix(path, "/api/recordings/"))
	case strings.HasPrefix(path, "/api/recordings/") && strings.HasSuffix(path, "/shadowing") && r.Method == http.MethodPost:
		s.routeShadowingPath(w, r, strings.TrimPrefix(path, "/api/recordings/"))
	case strings.HasPrefix(path, "/api/recordings/") && r.Method == http.MethodGet:
		s.handleGetRecording(w, r, strings.TrimPrefix(path, "/api/recordings/"))
	case strings.HasPrefix(path, "/api/recordings/") && r.Method == http.MethodDelete:
		s.handleDeleteRecording(w, r, strings.TrimPrefix(path, "/api/recordings/"))
	case path == "/api/recording-sessions" && r.Method == http.MethodPost:
		s.handleCreateRecordingSession(w, r)
	case strings.HasPrefix(path, "/api/recording-sessions/"):
		s.routeRecordingSessionPath(w, r, strings.TrimPrefix(path, "/api/recording-sessions/"))
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

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	logger := logging.ForRequest("api.auth.register", r)
	var payload struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	_ = json.NewDecoder(r.Body).Decode(&payload)
	creds, err := auth.ValidateCredentials(payload.Email, payload.Password)
	if err != nil {
		writeHTTPError(w, err, http.StatusBadRequest)
		return
	}
	user, err := auth.RegisterUser(r.Context(), s.db, creds.Email, creds.Password)
	if err != nil {
		writeHTTPError(w, err, http.StatusInternalServerError)
		return
	}
	session, err := auth.CreateSession(r.Context(), s.db, user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to register user."})
		return
	}
	http.SetCookie(w, auth.NewSessionCookieWithConfig(s.sessionCookie, session.Token, session.ExpiresAt))
	logger.Info("request.success", map[string]any{"status": 201, "durationMs": logging.ElapsedMs(started), "userId": user.ID})
	writeJSON(w, http.StatusCreated, map[string]any{"user": user})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	logger := logging.ForRequest("api.auth.login", r)
	var payload struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	_ = json.NewDecoder(r.Body).Decode(&payload)
	creds, err := auth.ValidateCredentials(payload.Email, payload.Password)
	if err != nil {
		writeHTTPError(w, err, http.StatusBadRequest)
		return
	}
	user, err := auth.LoginUser(r.Context(), s.db, creds.Email, creds.Password)
	if err != nil {
		writeHTTPError(w, err, http.StatusInternalServerError)
		return
	}
	session, err := auth.CreateSession(r.Context(), s.db, user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to sign in."})
		return
	}
	http.SetCookie(w, auth.NewSessionCookieWithConfig(s.sessionCookie, session.Token, session.ExpiresAt))
	logger.Info("request.success", map[string]any{"status": 200, "durationMs": logging.ElapsedMs(started), "userId": user.ID})
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	logger := logging.ForRequest("api.auth.session", r)
	user, ok := s.authorize(w, r, "api.auth.session", true)
	if !ok {
		return
	}
	logger.Info("request.success", map[string]any{"status": 200, "durationMs": logging.ElapsedMs(started), "userId": user.ID})
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	token := sessionToken(r)
	if err := auth.DeleteSessionByToken(r.Context(), s.db, token); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to sign out."})
		return
	}
	http.SetCookie(w, auth.ClearSessionCookieWithConfig(s.sessionCookie))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) authorizedUser(w http.ResponseWriter, r *http.Request, scope string) (*auth.User, bool) {
	return s.authorize(w, r, scope, false)
}

func (s *Server) authorize(w http.ResponseWriter, r *http.Request, scope string, clearInvalidSession bool) (*auth.User, bool) {
	started := time.Now()
	logger := logging.ForRequest(scope, r)
	writeError := func(status int, message string) {
		if clearInvalidSession && status == http.StatusUnauthorized {
			http.SetCookie(w, auth.ClearSessionCookieWithConfig(s.sessionCookie))
		}
		writeJSON(w, status, map[string]string{"error": message})
	}
	token := sessionToken(r)
	if token == "" {
		logger.Info("request.unauthorized", map[string]any{"status": 401, "durationMs": logging.ElapsedMs(started)})
		writeError(http.StatusUnauthorized, "Unauthorized")
		return nil, false
	}
	user, err := auth.GetUserBySessionToken(r.Context(), s.db, token)
	if err != nil {
		logger.Error("request.failed", logging.ErrorMeta(err))
		writeError(http.StatusInternalServerError, "Failed to load session.")
		return nil, false
	}
	if user == nil {
		logger.Info("request.unauthorized", map[string]any{"status": 401, "durationMs": logging.ElapsedMs(started)})
		writeError(http.StatusUnauthorized, "Unauthorized")
		return nil, false
	}
	return user, true
}

func sessionToken(r *http.Request) string {
	cookie, err := r.Cookie(auth.SessionCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func writeHTTPError(w http.ResponseWriter, err error, fallbackStatus int) {
	var httpErr auth.HTTPError
	if errors.As(err, &httpErr) {
		writeJSON(w, httpErr.Status, map[string]string{"error": httpErr.Message})
		return
	}
	writeJSON(w, fallbackStatus, map[string]string{"error": err.Error()})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
