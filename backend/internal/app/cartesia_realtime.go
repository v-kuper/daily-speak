package app

import (
	"os"

	"daily-speaking-practice/backend/internal/interview"
	"daily-speaking-practice/backend/internal/interview/cartesiaadapter"
)

func newCartesiaRealtimeIssuer() interview.RealtimeCredentialIssuer {
	return cartesiaadapter.New(cartesiaadapter.Config{
		APIKey:       os.Getenv("CARTESIA_API_KEY"),
		APIVersion:   os.Getenv("CARTESIA_API_VERSION"),
		WebSocketURL: os.Getenv("CARTESIA_STT_WEBSOCKET_URL"),
	})
}
