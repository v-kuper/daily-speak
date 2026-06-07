package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/auth"
	"daily-speaking-practice/backend/internal/db"
	"daily-speaking-practice/backend/internal/logging"
)

type Config struct {
	DB      *db.DB
	NextURL string
}

type Server struct {
	db        *db.DB
	nextProxy http.Handler
}

func NewServer(config Config) *Server {
	var proxy http.Handler = http.NotFoundHandler()
	if strings.TrimSpace(config.NextURL) != "" {
		if parsed, err := url.Parse(config.NextURL); err == nil {
			proxy = httputil.NewSingleHostReverseProxy(parsed)
		}
	}
	return &Server{db: config.DB, nextProxy: proxy}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.healthz)
	mux.HandleFunc("/api/", s.routeAPI)
	mux.Handle(uploadsURLPrefix, uploadsHandler())
	mux.Handle("/", s.nextProxy)
	return mux
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
	http.SetCookie(w, auth.NewSessionCookie(session.Token, session.ExpiresAt))
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
	http.SetCookie(w, auth.NewSessionCookie(session.Token, session.ExpiresAt))
	logger.Info("request.success", map[string]any{"status": 200, "durationMs": logging.ElapsedMs(started), "userId": user.ID})
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	logger := logging.ForRequest("api.auth.session", r)
	user, ok := s.authorizedUser(w, r, "api.auth.session")
	if !ok {
		http.SetCookie(w, auth.ClearSessionCookie())
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
	http.SetCookie(w, auth.ClearSessionCookie())
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) authorizedUser(w http.ResponseWriter, r *http.Request, scope string) (*auth.User, bool) {
	started := time.Now()
	logger := logging.ForRequest(scope, r)
	token := sessionToken(r)
	if token == "" {
		logger.Info("request.unauthorized", map[string]any{"status": 401, "durationMs": logging.ElapsedMs(started)})
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
		return nil, false
	}
	user, err := auth.GetUserBySessionToken(r.Context(), s.db, token)
	if err != nil {
		logger.Error("request.failed", logging.ErrorMeta(err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load session."})
		return nil, false
	}
	if user == nil {
		logger.Info("request.unauthorized", map[string]any{"status": 401, "durationMs": logging.ElapsedMs(started)})
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
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
