package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"net/mail"
	"os"
	"strconv"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/db"
	"daily-speaking-practice/backend/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/scrypt"
)

const (
	SessionCookieName = "daily_speaking_session"
	sessionTTL        = 30 * 24 * time.Hour
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

type Session struct {
	Token     string
	ExpiresAt time.Time
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
	return User{ID: id, Email: email, IsSubscriber: false, EnglishLevel: domain.DefaultEnglishLevel}, nil
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
	if errors.Is(err, pgx.ErrNoRows) || !VerifyPassword(password, row.PasswordHash) {
		return User{}, HTTPError{Message: "Invalid email or password.", Status: 401}
	}
	if err != nil {
		return User{}, err
	}
	level := domain.DefaultEnglishLevel
	if row.EnglishLevel != nil {
		level = domain.NormalizeEnglishLevel(*row.EnglishLevel)
	}
	return User{ID: row.ID, Email: row.Email, IsSubscriber: row.IsSubscriber, EnglishLevel: level}, nil
}

func CreateSession(ctx context.Context, database *db.DB, userID string) (Session, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return Session{}, err
	}
	token := hex.EncodeToString(tokenBytes)
	expiresAt := time.Now().UTC().Add(sessionTTL)
	_, err := database.Exec(ctx, `
		INSERT INTO user_sessions (id, user_id, token_hash, expires_at)
		VALUES ($1, $2, $3, $4)`, uuid.NewString(), userID, hashSessionToken(token), expiresAt)
	if err != nil {
		return Session{}, err
	}
	_, _ = database.Exec(ctx, `DELETE FROM user_sessions WHERE expires_at <= NOW()`)
	return Session{Token: token, ExpiresAt: expiresAt}, nil
}

func GetUserBySessionToken(ctx context.Context, database *db.DB, token string) (*User, error) {
	if strings.TrimSpace(token) == "" {
		return nil, nil
	}
	var row struct {
		UserID       string
		Email        string
		IsSubscriber bool
		EnglishLevel *string
	}
	err := database.QueryRow(ctx, `
		SELECT s.user_id,
		       u.email,
		       (u.is_subscriber AND (u.subscription_expires_at IS NULL OR u.subscription_expires_at > NOW())) AS is_subscriber,
		       u.english_level
		FROM user_sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1
		  AND s.expires_at > NOW()
		LIMIT 1`, hashSessionToken(token)).Scan(&row.UserID, &row.Email, &row.IsSubscriber, &row.EnglishLevel)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	level := domain.DefaultEnglishLevel
	if row.EnglishLevel != nil {
		level = domain.NormalizeEnglishLevel(*row.EnglishLevel)
	}
	return &User{ID: row.UserID, Email: row.Email, IsSubscriber: row.IsSubscriber, EnglishLevel: level}, nil
}

func DeleteSessionByToken(ctx context.Context, database *db.DB, token string) error {
	if strings.TrimSpace(token) == "" {
		return nil
	}
	_, err := database.Exec(ctx, `DELETE FROM user_sessions WHERE token_hash = $1`, hashSessionToken(token))
	return err
}

func NewSessionCookie(token string, expiresAt time.Time) *http.Cookie {
	return &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   os.Getenv("NODE_ENV") == "production",
		Expires:  expiresAt,
	}
}

func ClearSessionCookie() *http.Cookie {
	return &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   os.Getenv("NODE_ENV") == "production",
		MaxAge:   -1,
	}
}

func hashSessionToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
