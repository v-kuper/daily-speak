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

func TestInterviewTurnAggregateLimitsCoverThreeMinuteGuestPCM(t *testing.T) {
	if got := interviewTurnAudioTotalLimit("guest"); got != 8*1024*1024 {
		t.Fatalf("guest aggregate limit = %d", got)
	}
	threeMinutePCMBytes := int64(180 * 16000 * 2)
	if interviewTurnAudioTotalLimit("guest") < threeMinutePCMBytes {
		t.Fatalf("guest aggregate limit cannot hold three-minute 16 kHz mono PCM: limit=%d audio=%d",
			interviewTurnAudioTotalLimit("guest"), threeMinutePCMBytes)
	}
	if got := interviewTurnAudioTotalLimit("user"); got != 24*1024*1024 {
		t.Fatalf("account aggregate limit = %d", got)
	}
}
