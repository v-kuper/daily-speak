package domain

import "testing"

func TestNormalizeStoredShadowingAudioSourceAcceptsStoredMP3(t *testing.T) {
	want := "/uploads/shadowing/user-1/recording-1.mp3"
	got := NormalizeStoredShadowingAudioSource("  " + want + "  ")
	if got == nil || *got != want {
		t.Fatalf("expected %q, got %#v", want, got)
	}
}

func TestNormalizeStoredShadowingAudioSourceRejectsUnsafeOrUnexpectedSources(t *testing.T) {
	values := []string{
		"data:audio/mpeg;base64,AAAA",
		"https://example.com/audio.mp3",
		"/uploads/shadowing/user-1/recording-1.wav",
		"/uploads/recordings/user-1/recording-1.mp3",
		"/uploads/shadowing/user-1/nested/recording-1.mp3",
		"/uploads/shadowing/../recording-1.mp3",
	}
	for _, value := range values {
		if got := NormalizeStoredShadowingAudioSource(value); got != nil {
			t.Fatalf("expected %q to be rejected, got %q", value, *got)
		}
	}
}
