package activity

import (
	"context"
	"errors"
	"testing"
	"time"
)

type repositoryStub struct {
	calls   int
	summary Summary
	window  Window
}

func (r *repositoryStub) Append(context.Context, string, []Interval) error { r.calls++; return nil }
func (r *repositoryStub) Summary(_ context.Context, _ string, window Window) (Summary, error) {
	r.window = window
	return r.summary, nil
}

func TestActivityValidationBeforePersistence(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	valid := Interval{ID: "event-12345678", Kind: "speaking", StartedAt: now.Add(-30 * time.Second), EndedAt: now}
	for _, test := range []struct {
		name   string
		change func(*Interval)
	}{
		{"short key", func(i *Interval) { i.ID = "tiny" }},
		{"unsupported kind", func(i *Interval) { i.Kind = "idle" }},
		{"zero start", func(i *Interval) { i.StartedAt = time.Time{} }},
		{"negative duration", func(i *Interval) { i.StartedAt = now.Add(time.Second) }},
		{"long chunk", func(i *Interval) { i.StartedAt = now.Add(-31 * time.Second) }},
		{"future", func(i *Interval) { i.StartedAt = now.Add(11 * time.Second); i.EndedAt = now.Add(12 * time.Second) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := &repositoryStub{}
			service := NewService(repo)
			service.now = func() time.Time { return now }
			input := valid
			test.change(&input)
			if err := service.Record(context.Background(), "user", []Interval{input}); !errors.Is(err, ErrInvalid) || repo.calls != 0 {
				t.Fatalf("err=%v writes=%d", err, repo.calls)
			}
		})
	}
	repo := &repositoryStub{}
	service := NewService(repo)
	service.now = func() time.Time { return now }
	if err := service.Record(context.Background(), "user", []Interval{valid}); err != nil || repo.calls != 1 {
		t.Fatalf("err=%v calls=%d", err, repo.calls)
	}
	for _, input := range [][]Interval{nil, make([]Interval, 101)} {
		if err := service.Record(context.Background(), "user", input); !errors.Is(err, ErrInvalid) {
			t.Fatalf("err=%v", err)
		}
	}
}

func TestUncoveredUnionsUnorderedAndNestedIntervals(t *testing.T) {
	base := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	span := func(from, to int) Interval {
		return Interval{StartedAt: base.Add(time.Duration(from) * time.Second), EndedAt: base.Add(time.Duration(to) * time.Second)}
	}
	input := span(0, 30)
	pieces := uncovered(input, []Interval{span(20, 40), span(6, 8), span(-5, 5), span(5, 10), span(15, 18)})
	if len(pieces) != 2 || !pieces[0].StartedAt.Equal(base.Add(10*time.Second)) || !pieces[0].EndedAt.Equal(base.Add(15*time.Second)) ||
		!pieces[1].StartedAt.Equal(base.Add(18*time.Second)) || !pieces[1].EndedAt.Equal(base.Add(20*time.Second)) {
		t.Fatalf("pieces=%+v", pieces)
	}
	if len(uncovered(input, []Interval{span(-1, 31)})) != 0 {
		t.Fatal("fully covered interval earned time")
	}
}

func TestIntensityUsesExactBoundariesAndIncludesShortAnswers(t *testing.T) {
	for _, test := range []struct {
		ms    int64
		level int
	}{{0, 0}, {1, 1}, {60_000, 1}, {300_000, 1}, {300_001, 2}, {899_999, 2}, {900_000, 3}} {
		if actual := Intensity(test.ms); actual != test.level {
			t.Fatalf("%dms: level=%d", test.ms, actual)
		}
	}
}

func TestSummaryUsesLearnerCalendarAndLifetimeCounter(t *testing.T) {
	repo := &repositoryStub{summary: Summary{TotalSpeakingMilliseconds: 5_000_000, Days: []Day{{Date: "2026-10-02", SpeakingMilliseconds: 300_000, ReviewMilliseconds: 1}}}}
	service := NewService(repo)
	service.now = func() time.Time { return time.Date(2026, 10, 1, 22, 0, 0, 0, time.UTC) }
	result, err := service.Summary(context.Background(), "user", "Europe/Minsk")
	if err != nil || len(result.Days) != 366 || result.To != "2026-10-02" || result.TotalSpeakingMilliseconds != 5_000_000 || result.Days[365].Level != 2 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if repo.window.To.Hour() != 0 || repo.window.Timezone != "Europe/Minsk" {
		t.Fatalf("window=%+v", repo.window)
	}
	for _, timezone := range []string{"Local", "MadeUp/Timezone"} {
		if _, err := service.Summary(context.Background(), "user", timezone); !errors.Is(err, ErrInvalid) {
			t.Fatalf("timezone=%s err=%v", timezone, err)
		}
	}
}
