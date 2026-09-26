package storage

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const defaultPresignTTL = 15 * time.Minute

const (
	defaultMultipartPartSize = int64(8 * 1024 * 1024)
	minimumMultipartPartSize = int64(5 * 1024 * 1024)
	maximumMultipartPartSize = int64(5 * 1024 * 1024 * 1024)
)

type Config struct {
	Backend string

	LocalDir string

	S3Bucket          string
	S3Region          string
	S3Endpoint        string
	S3AccessKeyID     string
	S3SecretAccessKey string
	S3SessionToken    string
	S3ForcePathStyle  bool
	KeyPrefix         string
	PresignTTL        time.Duration
	MultipartPartSize int64
}

// ConfigFromEnv defaults to local storage so the existing Windows deployment
// continues to use UPLOADS_DIR and its UPLOADS_HOST_DIR bind mount without any
// S3 secrets. S3 settings become mandatory only when MEDIA_STORAGE_DRIVER=s3.
func ConfigFromEnv() (Config, error) {
	backend := strings.ToLower(strings.TrimSpace(os.Getenv("MEDIA_STORAGE_DRIVER")))
	if backend == "" {
		backend = BackendLocal
	}
	config := Config{
		Backend:           backend,
		LocalDir:          firstNonEmptyEnv("MEDIA_LOCAL_DIR", "UPLOADS_DIR"),
		S3Bucket:          strings.TrimSpace(os.Getenv("MEDIA_S3_BUCKET")),
		S3Region:          strings.TrimSpace(os.Getenv("MEDIA_S3_REGION")),
		S3Endpoint:        strings.TrimSpace(os.Getenv("MEDIA_S3_ENDPOINT")),
		S3AccessKeyID:     strings.TrimSpace(os.Getenv("MEDIA_S3_ACCESS_KEY_ID")),
		S3SecretAccessKey: strings.TrimSpace(os.Getenv("MEDIA_S3_SECRET_ACCESS_KEY")),
		S3SessionToken:    strings.TrimSpace(os.Getenv("MEDIA_S3_SESSION_TOKEN")),
		KeyPrefix:         strings.Trim(strings.TrimSpace(os.Getenv("MEDIA_STORAGE_KEY_PREFIX")), "/"),
		PresignTTL:        defaultPresignTTL,
		MultipartPartSize: defaultMultipartPartSize,
	}
	if config.LocalDir == "" {
		config.LocalDir = filepath.Join("public", "uploads")
	}
	var err error
	if config.S3ForcePathStyle, err = optionalBoolEnv("MEDIA_S3_FORCE_PATH_STYLE", false); err != nil {
		return Config{}, err
	}
	if value := strings.TrimSpace(os.Getenv("MEDIA_UPLOAD_URL_TTL")); value != "" {
		config.PresignTTL, err = time.ParseDuration(value)
		if err != nil {
			return Config{}, fmt.Errorf("MEDIA_UPLOAD_URL_TTL must be a duration: %w", err)
		}
	}
	if value := strings.TrimSpace(os.Getenv("MEDIA_MULTIPART_PART_SIZE_BYTES")); value != "" {
		config.MultipartPartSize, err = strconv.ParseInt(value, 10, 64)
		if err != nil {
			return Config{}, errors.New("MEDIA_MULTIPART_PART_SIZE_BYTES must be an integer")
		}
	}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (config Config) Validate() error {
	config = config.withDefaults()
	if config.Backend != BackendLocal && config.Backend != BackendS3 {
		return errors.New("MEDIA_STORAGE_DRIVER must be local or s3")
	}
	if _, err := normalizeTTL(config.PresignTTL, defaultPresignTTL); err != nil {
		return errors.New("MEDIA_UPLOAD_URL_TTL must be between 1m and 24h")
	}
	if config.MultipartPartSize < minimumMultipartPartSize || config.MultipartPartSize > maximumMultipartPartSize {
		return errors.New("MEDIA_MULTIPART_PART_SIZE_BYTES must be between 5242880 and 5368709120")
	}
	if config.KeyPrefix != "" {
		if strings.Trim(config.KeyPrefix, "/") != config.KeyPrefix || path.Clean(config.KeyPrefix) != config.KeyPrefix {
			return errors.New("MEDIA_STORAGE_KEY_PREFIX must be a relative object-key prefix")
		}
		for _, segment := range strings.Split(config.KeyPrefix, "/") {
			if !objectKeySegmentPattern.MatchString(segment) {
				return errors.New("MEDIA_STORAGE_KEY_PREFIX contains an invalid segment")
			}
		}
	}
	if config.Backend == BackendLocal {
		if strings.TrimSpace(config.LocalDir) == "" {
			return errors.New("MEDIA_LOCAL_DIR or UPLOADS_DIR is required for local storage")
		}
		return nil
	}
	if config.S3Bucket == "" || strings.ContainsAny(config.S3Bucket, "/\\\r\n\x00") {
		return errors.New("MEDIA_S3_BUCKET is required for s3 storage")
	}
	if config.S3Region == "" || strings.ContainsAny(config.S3Region, "\r\n\x00") {
		return errors.New("MEDIA_S3_REGION is required for s3 storage")
	}
	if (config.S3AccessKeyID == "") != (config.S3SecretAccessKey == "") {
		return errors.New("MEDIA_S3_ACCESS_KEY_ID and MEDIA_S3_SECRET_ACCESS_KEY must be set together")
	}
	if config.S3SessionToken != "" && config.S3AccessKeyID == "" {
		return errors.New("MEDIA_S3_SESSION_TOKEN requires explicit S3 credentials")
	}
	if config.S3Endpoint != "" {
		parsed, err := url.Parse(config.S3Endpoint)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
			return errors.New("MEDIA_S3_ENDPOINT must be an http(s) origin without credentials, query, fragment, or path")
		}
	}
	return nil
}

func (config Config) withDefaults() Config {
	if config.Backend == "" {
		config.Backend = BackendLocal
	}
	if config.LocalDir == "" {
		config.LocalDir = filepath.Join("public", "uploads")
	}
	if config.PresignTTL == 0 {
		config.PresignTTL = defaultPresignTTL
	}
	if config.MultipartPartSize == 0 {
		config.MultipartPartSize = defaultMultipartPartSize
	}
	return config
}

func firstNonEmptyEnv(names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}

func optionalBoolEnv(name string, fallback bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false", name)
	}
	return parsed, nil
}
