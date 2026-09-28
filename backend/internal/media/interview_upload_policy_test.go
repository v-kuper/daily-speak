package media

import (
	"errors"
	"strings"
	"testing"
)

func TestInterviewTurnUploadRequiresSessionAndWAV(t *testing.T) {
	input := CreateUploadInput{
		OwnerPrincipalID: "guest", OwnerKind: "guest", SessionID: "device",
		IdempotencyKey: "upload-12345678", Purpose: PurposeInterviewTurnAudio,
		ContentType: "audio/wav", SizeBytes: 1024,
		ChecksumSHA256: strings.Repeat("a", 64),
	}
	owned, err := applyCreateOwnerPolicy(normalizeCreateInput(input))
	if err != nil || owned.Purpose != PurposeInterviewTurnAudio {
		t.Fatalf("guest turn purpose = %q, err=%v", owned.Purpose, err)
	}
	if _, err := validateCreateInput(owned); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("missing interview session err=%v", err)
	}
	owned.InterviewSessionID = "interview"
	if _, err := validateCreateInput(owned); err != nil {
		t.Fatalf("valid WAV rejected: %v", err)
	}
	owned.ContentType = "audio/webm"
	if _, err := validateCreateInput(owned); !errors.Is(err, ErrUnsupportedType) {
		t.Fatalf("non-WAV turn audio err=%v", err)
	}
}
