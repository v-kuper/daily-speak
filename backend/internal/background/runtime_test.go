package background

import (
	"context"
	"strings"
	"testing"

	"daily-speaking-practice/backend/internal/guestpreview"
	"daily-speaking-practice/backend/internal/recording"
	"daily-speaking-practice/backend/internal/shadowing"
	"daily-speaking-practice/backend/internal/workqueue"
	"github.com/jackc/pgx/v5"
)

type recordingProcessor struct{ job recording.ProcessingJob }

func (p *recordingProcessor) Process(_ context.Context, job recording.ProcessingJob, _ recording.AnalysisLogger) error {
	p.job = job
	return nil
}

type guestProcessor struct{ job guestpreview.Job }

func (p *guestProcessor) Process(_ context.Context, job guestpreview.Job) error {
	p.job = job
	return nil
}

type shadowProcessor struct{ job shadowing.Job }

func (p *shadowProcessor) Process(_ context.Context, job shadowing.Job, _ shadowing.Logger) error {
	p.job = job
	return nil
}

type cleanup struct{ resourceID string }

func (*cleanup) Available() bool                            { return true }
func (*cleanup) AbortExpiredUploads(context.Context) error  { return nil }
func (*cleanup) EnqueueExpiredAssets(context.Context) error { return nil }
func (c *cleanup) Delete(_ context.Context, resourceID string) error {
	c.resourceID = resourceID
	return nil
}
func (*cleanup) FinalizeFailure(context.Context, pgx.Tx, string, int, string) error { return nil }

func TestRuntimeDispatchesJobsWithoutHTTPTransport(t *testing.T) {
	recordings := &recordingProcessor{}
	strengths := &recordingProcessor{}
	guests := &guestProcessor{}
	shadows := &shadowProcessor{}
	mediaCleanup := &cleanup{}
	runtime := NewRuntime(Dependencies{
		RecordingProcessor: recordings, StrengthsProcessor: strengths, GuestPreviewProcessor: guests,
		ShadowingProcessor: shadows, MediaCleanup: mediaCleanup,
	})
	jobs := []workqueue.Job{
		{ID: "guest-job", Kind: workqueue.KindGuestPreview, ResourceID: "preview", LeaseToken: "g-lease"},
		{ID: "recording-job", Kind: workqueue.KindRecordingProcess, ResourceID: "recording", LeaseToken: "r-lease"},
		{ID: "strengths-job", Kind: workqueue.KindRecordingStrengths, ResourceID: "recording", LeaseToken: "p-lease"},
		{ID: "shadow-job", Kind: workqueue.KindShadowingSynthesize, ResourceID: "recording", LeaseToken: "s-lease"},
		{ID: "delete-job", Kind: workqueue.KindMediaDelete, ResourceID: "asset"},
	}
	for _, job := range jobs {
		if err := runtime.Handle(context.Background(), job); err != nil {
			t.Fatalf("%s: %v", job.Kind, err)
		}
	}
	if strengths.job.ID != "strengths-job" || strengths.job.LeaseToken != "p-lease" || guests.job.ID != "guest-job" || recordings.job.ID != "recording-job" || shadows.job.ID != "shadow-job" || mediaCleanup.resourceID != "asset" {
		t.Fatalf("guest=%#v recording=%#v shadow=%#v cleanup=%#v", guests.job, recordings.job, shadows.job, mediaCleanup)
	}
}

func TestRuntimeRejectsUnsupportedOrUnconfiguredJobs(t *testing.T) {
	runtime := NewRuntime(Dependencies{})
	if err := runtime.Handle(context.Background(), workqueue.Job{Kind: "unknown"}); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("err=%v", err)
	}
	if err := runtime.Handle(context.Background(), workqueue.Job{Kind: workqueue.KindRecordingProcess}); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("err=%v", err)
	}
	if err := runtime.FinalizeFailure(context.Background(), nil, workqueue.Job{Kind: workqueue.KindRecordingProcess}, "failed"); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("finalize err=%v", err)
	}
}
