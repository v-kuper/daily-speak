package storage

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
)

func TestS3ChecksumEncodingRoundTrip(t *testing.T) {
	hash := sha256.Sum256([]byte("media"))
	hexValue := checksum([]byte("media"))
	base64Value, err := checksumHexToBase64(hexValue)
	if err != nil {
		t.Fatal(err)
	}
	if base64Value != base64.StdEncoding.EncodeToString(hash[:]) {
		t.Fatalf("base64 checksum = %q", base64Value)
	}
	decoded, err := checksumBase64ToHex(base64Value)
	if err != nil || decoded != hexValue {
		t.Fatalf("round trip = %q, %v", decoded, err)
	}
}

func TestS3RemoteKeyAppliesConfiguredPrefixWithoutChangingLogicalKey(t *testing.T) {
	store := &S3Store{prefix: "daily-speaking/prod"}
	logical := "v1/user/recording/file.webm"
	if got := store.remoteKey(logical); got != "daily-speaking/prod/"+logical {
		t.Fatalf("remote key = %q", got)
	}
	if strings.HasPrefix(logical, store.prefix) {
		t.Fatal("test logical key unexpectedly includes provider prefix")
	}
}
