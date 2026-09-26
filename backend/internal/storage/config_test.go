package storage

import (
	"path/filepath"
	"testing"
	"time"
)

func TestConfigFromEnvDefaultsToLegacyCompatibleLocalStorage(t *testing.T) {
	clearStorageEnv(t)
	uploadsDir := filepath.Join(t.TempDir(), "windows-volume")
	t.Setenv("UPLOADS_DIR", uploadsDir)

	config, err := ConfigFromEnv()
	if err != nil {
		t.Fatalf("ConfigFromEnv: %v", err)
	}
	if config.Backend != BackendLocal {
		t.Fatalf("backend = %q, want local", config.Backend)
	}
	if config.LocalDir != uploadsDir {
		t.Fatalf("local dir = %q, want existing UPLOADS_DIR %q", config.LocalDir, uploadsDir)
	}
	if config.S3Bucket != "" || config.S3AccessKeyID != "" || config.S3SecretAccessKey != "" {
		t.Fatal("local default unexpectedly requires S3 configuration")
	}
	if config.PresignTTL != 15*time.Minute {
		t.Fatalf("presign TTL = %s", config.PresignTTL)
	}
	if config.MultipartPartSize != 8*1024*1024 {
		t.Fatalf("part size = %d", config.MultipartPartSize)
	}
}

func TestConfigFromEnvMediaLocalDirOverridesUploadsDir(t *testing.T) {
	clearStorageEnv(t)
	t.Setenv("UPLOADS_DIR", "legacy")
	t.Setenv("MEDIA_LOCAL_DIR", "media")
	config, err := ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if config.LocalDir != "media" {
		t.Fatalf("local dir = %q", config.LocalDir)
	}
}

func TestConfigFromEnvRequiresS3SettingsOnlyForS3Driver(t *testing.T) {
	clearStorageEnv(t)
	t.Setenv("MEDIA_STORAGE_DRIVER", "s3")
	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("expected missing S3 bucket and region to fail")
	}

	t.Setenv("MEDIA_S3_BUCKET", "media")
	t.Setenv("MEDIA_S3_REGION", "us-east-1")
	t.Setenv("MEDIA_S3_ENDPOINT", "http://127.0.0.1:9000")
	t.Setenv("MEDIA_S3_ACCESS_KEY_ID", "key")
	t.Setenv("MEDIA_S3_SECRET_ACCESS_KEY", "secret")
	t.Setenv("MEDIA_S3_FORCE_PATH_STYLE", "true")
	t.Setenv("MEDIA_STORAGE_KEY_PREFIX", "daily-speaking/prod")
	t.Setenv("MEDIA_UPLOAD_URL_TTL", "10m")
	t.Setenv("MEDIA_MULTIPART_PART_SIZE_BYTES", "10485760")
	config, err := ConfigFromEnv()
	if err != nil {
		t.Fatalf("valid S3 config: %v", err)
	}
	if config.Backend != BackendS3 || !config.S3ForcePathStyle || config.PresignTTL != 10*time.Minute || config.MultipartPartSize != 10*1024*1024 {
		t.Fatalf("unexpected S3 config: %#v", config)
	}
}

func TestConfigFromEnvRejectsPartialCredentialsAndInvalidLimits(t *testing.T) {
	for name, value := range map[string]string{
		"MEDIA_S3_ACCESS_KEY_ID":          "key",
		"MEDIA_S3_FORCE_PATH_STYLE":       "sometimes",
		"MEDIA_UPLOAD_URL_TTL":            "30s",
		"MEDIA_MULTIPART_PART_SIZE_BYTES": "1024",
	} {
		t.Run(name, func(t *testing.T) {
			clearStorageEnv(t)
			t.Setenv("MEDIA_STORAGE_DRIVER", "s3")
			t.Setenv("MEDIA_S3_BUCKET", "media")
			t.Setenv("MEDIA_S3_REGION", "us-east-1")
			t.Setenv(name, value)
			if _, err := ConfigFromEnv(); err == nil {
				t.Fatalf("expected %s=%q to fail", name, value)
			}
		})
	}
}

func clearStorageEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"MEDIA_STORAGE_DRIVER", "MEDIA_LOCAL_DIR", "UPLOADS_DIR",
		"MEDIA_S3_BUCKET", "MEDIA_S3_REGION", "MEDIA_S3_ENDPOINT",
		"MEDIA_S3_ACCESS_KEY_ID", "MEDIA_S3_SECRET_ACCESS_KEY", "MEDIA_S3_SESSION_TOKEN",
		"MEDIA_S3_FORCE_PATH_STYLE", "MEDIA_STORAGE_KEY_PREFIX", "MEDIA_UPLOAD_URL_TTL",
		"MEDIA_MULTIPART_PART_SIZE_BYTES",
	} {
		t.Setenv(name, "")
	}
}
