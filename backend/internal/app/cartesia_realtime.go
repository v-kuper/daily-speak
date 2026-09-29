package app

import (
	"os"

	"daily-speaking-practice/backend/internal/interview"
	"daily-speaking-practice/backend/internal/interview/cartesiaadapter"
)

func newCartesiaRealtimeIssuer() interview.CredentialIssuer {
	return cartesiaadapter.New(cartesiaadapter.Config{
		APIKey:       os.Getenv("CARTESIA_API_KEY"),
		APIVersion:   os.Getenv("CARTESIA_API_VERSION"),
		WebSocketURL: os.Getenv("CARTESIA_STT_WEBSOCKET_URL"),
		TTSAPIURL:    os.Getenv("CARTESIA_API_URL"),
		TTSModel:     os.Getenv("CARTESIA_MODEL"),
		VoiceID:      os.Getenv("CARTESIA_VOICE_ID"),
	})
}
