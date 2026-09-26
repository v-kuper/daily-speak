package storage

import (
	"errors"
	"strings"
	"testing"
)

func TestNewObjectKeyCreatesSafeOpaqueKey(t *testing.T) {
	first, err := NewObjectKey("principal-123", "recording_audio", ".WEBM")
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewObjectKey("principal-123", "recording_audio", "webm")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("object keys must be unique")
	}
	if !strings.HasPrefix(first, "v1/principal-123/recording_audio/") || !strings.HasSuffix(first, ".webm") {
		t.Fatalf("unexpected key %q", first)
	}
	if err := ValidateObjectKey(first); err != nil {
		t.Fatalf("generated key is invalid: %v", err)
	}
}

func TestObjectKeysRejectTraversalAndClientPaths(t *testing.T) {
	for _, key := range []string{"", "/absolute", "../secret", "a/../secret", `a\\secret`, "a//b", "a/./b", "a/b/", " a/b", "a/\nb"} {
		if err := ValidateObjectKey(key); !errors.Is(err, ErrInvalidKey) {
			t.Fatalf("ValidateObjectKey(%q) = %v, want ErrInvalidKey", key, err)
		}
	}
	if _, err := NewObjectKey("../../owner", "recording", "webm"); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("unsafe principal error = %v", err)
	}
}
