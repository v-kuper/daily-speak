package operations

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestMetricsAreBoundedAndContainNoRequestData(t *testing.T) {
	metrics := NewMetrics()
	metrics.Observe("GET", "/api/v1/recordings/{id}", 200, 12*time.Millisecond)
	var output bytes.Buffer
	metrics.WritePrometheus(&output, DatabaseSnapshot{TotalConns: 3, IdleConns: 2, AcquiredConns: 1, MaxConns: 10}, []QueueSnapshot{{
		Kind: "recording.process", State: "queued", Count: 4, OldestAgeSeconds: 3,
	}})
	text := output.String()
	for _, expected := range []string{
		`route="/api/v1/recordings/{id}"`, `status="200"`, `daily_speaking_db_pool_connections{state="idle"} 2`,
		`daily_speaking_jobs{kind="recording.process",state="queued"} 4`,
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("missing %q in:\n%s", expected, text)
		}
	}
	for _, forbidden := range []string{"Authorization", "transcript", "user@example.com"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("metrics leaked %q", forbidden)
		}
	}
}

func TestMetricsSeparateActiveDepthFromRecentTerminalOutcomes(t *testing.T) {
	metrics := NewMetrics()
	var output bytes.Buffer
	metrics.WritePrometheus(&output, DatabaseSnapshot{}, []QueueSnapshot{
		{Kind: "recording.process", State: "queued", Count: 4, OldestAgeSeconds: 3},
		{Kind: "recording.process", State: "failed", Count: 2, RecentTerminal: true},
	})
	text := output.String()
	if !strings.Contains(text, `daily_speaking_jobs{kind="recording.process",state="queued"} 4`) ||
		!strings.Contains(text, `daily_speaking_jobs_completed_last_hour{kind="recording.process",state="failed"} 2`) {
		t.Fatalf("queue metrics:\n%s", text)
	}
}

func TestLimiterHelpersUseStableScopedHashesAndWindows(t *testing.T) {
	if subjectHash("auth", "ip:127.0.0.1") == subjectHash("write", "ip:127.0.0.1") {
		t.Fatal("scopes must not share counters")
	}
	if hash := subjectHash("auth", "ip:127.0.0.1"); len(hash) != 64 || strings.Contains(hash, "127.0.0.1") {
		t.Fatalf("unsafe hash %q", hash)
	}
	value := time.Date(2026, 9, 27, 12, 34, 56, 0, time.UTC)
	if got := floorTime(value, time.Minute); !got.Equal(time.Date(2026, 9, 27, 12, 34, 0, 0, time.UTC)) {
		t.Fatalf("window start = %s", got)
	}
}
