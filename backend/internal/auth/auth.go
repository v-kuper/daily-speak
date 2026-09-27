package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"os"
	"strconv"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/db"
	"daily-speaking-practice/backend/internal/learner"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/scrypt"
)

const (
	RefreshCookieName = "daily_speaking_refresh"
	passwordMinLength = 8
	scryptN           = 16384
	scryptR           = 8
	scryptP           = 1
	scryptKeyLength   = 64
)

type Credentials struct {
	Email    string
	Password string
}

type User struct {
	ID           string `json:"-"`
	Email        string `json:"email"`
	IsSubscriber bool   `json:"isSubscriber"`
	EnglishLevel string `json:"englishLevel"`
}

type HTTPError struct {
	Message string
	Status  int
}

func (e HTTPError) Error() string {
	return e.Message
}

func ValidateCredentials(email string, password string) (Credentials, error) {
	normalizedEmail := strings.ToLower(strings.TrimSpace(email))
	if _, err := mail.ParseAddress(normalizedEmail); err != nil || strings.Contains(normalizedEmail, " ") || !strings.Contains(normalizedEmail, ".") {
		return Credentials{}, HTTPError{Message: "Enter a valid email address.", Status: 400}
	}
	normalizedPassword := strings.TrimSpace(password)
	if len(normalizedPassword) < passwordMinLength {
		return Credentials{}, HTTPError{Message: "Password must be at least 8 characters.", Status: 400}
	}
	return Credentials{Email: normalizedEmail, Password: normalizedPassword}, nil
}

func HashPassword(password string) (string, error) {
	saltBytes := make([]byte, 16)
	if _, err := rand.Read(saltBytes); err != nil {
		return "", err
	}
	salt := hex.EncodeToString(saltBytes)
	key, err := scrypt.Key([]byte(password), []byte(salt), scryptN, scryptR, scryptP, scryptKeyLength)
	if err != nil {
		return "", err
	}
	return strings.Join([]string{"scrypt", strconv.Itoa(scryptN), strconv.Itoa(scryptR), strconv.Itoa(scryptP), salt, hex.EncodeToString(key)}, "$"), nil
}

func VerifyPassword(password string, encodedHash string) bool {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 || parts[0] != "scrypt" {
		return false
	}
	n, errN := strconv.Atoi(parts[1])
	r, errR := strconv.Atoi(parts[2])
	p, errP := strconv.Atoi(parts[3])
	if errN != nil || errR != nil || errP != nil || parts[4] == "" || parts[5] == "" {
		return false
	}
	stored, err := hex.DecodeString(parts[5])
	if err != nil || len(stored) == 0 {
		return false
	}
	computed, err := scrypt.Key([]byte(password), []byte(parts[4]), n, r, p, len(stored))
	if err != nil || len(computed) != len(stored) {
		return false
	}
	return subtle.ConstantTimeCompare(computed, stored) == 1
}

func RegisterUser(ctx context.Context, database *db.DB, email string, password string) (User, error) {
	passwordHash, err := HashPassword(password)
	if err != nil {
		return User{}, err
	}
	id := uuid.NewString()
	_, err = database.Exec(ctx, `INSERT INTO users (id, email, password_hash) VALUES ($1, $2, $3)`, id, email, passwordHash)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return User{}, HTTPError{Message: "User with this email already exists.", Status: 409}
		}
		return User{}, err
	}
	return User{ID: id, Email: email, IsSubscriber: false, EnglishLevel: learner.DefaultEnglishLevel}, nil
}

func LoginUser(ctx context.Context, database *db.DB, email string, password string) (User, error) {
	var row struct {
		ID           string
		Email        string
		PasswordHash string
		IsSubscriber bool
		EnglishLevel *string
	}
	err := database.QueryRow(ctx, `
		SELECT id, email, password_hash,
		       (is_subscriber AND (subscription_expires_at IS NULL OR subscription_expires_at > NOW())) AS is_subscriber,
		       english_level
		FROM users
		WHERE email = $1
		LIMIT 1`, email).Scan(&row.ID, &row.Email, &row.PasswordHash, &row.IsSubscriber, &row.EnglishLevel)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, HTTPError{Message: "Invalid email or password.", Status: 401}
	}
	if err != nil {
		return User{}, err
	}
	if !VerifyPassword(password, row.PasswordHash) {
		return User{}, HTTPError{Message: "Invalid email or password.", Status: 401}
	}
	level := learner.DefaultEnglishLevel
	if row.EnglishLevel != nil {
		level = learner.NormalizeEnglishLevel(*row.EnglishLevel)
	}
	return User{ID: row.ID, Email: row.Email, IsSubscriber: row.IsSubscriber, EnglishLevel: level}, nil
}

type CookieConfig struct {
	Secure   bool
	SameSite http.SameSite
	Domain   string
}

func CookieConfigFromEnv() (CookieConfig, error) {
	config := CookieConfig{
		SameSite: http.SameSiteLaxMode,
		Domain:   strings.TrimSpace(os.Getenv("SESSION_COOKIE_DOMAIN")),
	}
	if raw := strings.TrimSpace(os.Getenv("SESSION_COOKIE_SECURE")); raw != "" {
		secure, err := strconv.ParseBool(raw)
		if err != nil {
			return CookieConfig{}, fmt.Errorf("SESSION_COOKIE_SECURE must be a boolean")
		}
		config.Secure = secure
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("SESSION_COOKIE_SAME_SITE"))) {
	case "", "lax":
	case "strict":
		config.SameSite = http.SameSiteStrictMode
	case "none":
		config.SameSite = http.SameSiteNoneMode
	default:
		return CookieConfig{}, fmt.Errorf("SESSION_COOKIE_SAME_SITE must be lax, strict, or none")
	}
	if config.SameSite == http.SameSiteNoneMode && !config.Secure {
		return CookieConfig{}, fmt.Errorf("SESSION_COOKIE_SAME_SITE=none requires SESSION_COOKIE_SECURE=true")
	}
	if err := NewRefreshCookieWithConfig(config, "", time.Now().Add(time.Hour)).Valid(); err != nil {
		return CookieConfig{}, fmt.Errorf("SESSION_COOKIE_DOMAIN must be a valid cookie domain")
	}
	return config, nil
}

// NewRefreshCookieWithConfig keeps browser refresh credentials out of
// JavaScript. Native clients receive the same rotating credential in the JSON
// identity response and store it in OS-protected storage instead.
func NewRefreshCookieWithConfig(config CookieConfig, token string, expiresAt time.Time) *http.Cookie {
	return &http.Cookie{
		Name:     RefreshCookieName,
		Value:    token,
		Path:     "/api/v1/auth",
		Domain:   config.Domain,
		Expires:  expiresAt.UTC(),
		MaxAge:   int(time.Until(expiresAt).Seconds()),
		HttpOnly: true,
		Secure:   config.Secure,
		SameSite: config.SameSite,
	}
}

func ClearRefreshCookieWithConfig(config CookieConfig) *http.Cookie {
	return &http.Cookie{
		Name:     RefreshCookieName,
		Value:    "",
		Path:     "/api/v1/auth",
		Domain:   config.Domain,
		Expires:  time.Unix(1, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   config.Secure,
		SameSite: config.SameSite,
	}
}
