package activity

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"
	_ "time/tzdata"
)

var (
	ErrInvalid  = errors.New("invalid activity")
	ErrConflict = errors.New("activity key was already used for another interval")
	ErrAccount  = errors.New("activity account not found")
)

const MaxInterval = 30 * time.Second

type Interval struct {
	ID        string
	Kind      string
	StartedAt time.Time
	EndedAt   time.Time
}

type Day struct {
	Date                 string
	SpeakingMilliseconds int64
	ReviewMilliseconds   int64
	Level                int
}

type Summary struct {
	TotalSpeakingMilliseconds      int64
	HistoricalSpeakingMilliseconds int64
	Timezone                       string
	From                           string
	To                             string
	Days                           []Day
}

type Window struct {
	From     time.Time
	To       time.Time // Exclusive local midnight after the last displayed day.
	Timezone string
}

type Repository interface {
	Append(context.Context, string, []Interval) error
	Summary(context.Context, string, Window) (Summary, error)
}

type Service struct {
	repository Repository
	now        func() time.Time
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository, now: time.Now}
}

func (s *Service) Record(ctx context.Context, userID string, intervals []Interval) error {
	if userID == "" || len(intervals) == 0 || len(intervals) > 100 {
		return ErrInvalid
	}
	now := s.now()
	for i := range intervals {
		item := &intervals[i]
		item.StartedAt = item.StartedAt.UTC().Truncate(time.Millisecond)
		item.EndedAt = item.EndedAt.UTC().Truncate(time.Millisecond)
		if len(item.ID) < 8 || len(item.ID) > 120 || strings.TrimSpace(item.ID) != item.ID ||
			(item.Kind != "speaking" && item.Kind != "review") || item.StartedAt.IsZero() ||
			!item.EndedAt.After(item.StartedAt) || item.EndedAt.Sub(item.StartedAt) > MaxInterval ||
			item.EndedAt.After(now.Add(10*time.Second)) {
			return ErrInvalid
		}
	}
	return s.repository.Append(ctx, userID, intervals)
}

func (s *Service) Summary(ctx context.Context, userID, timezone string) (Summary, error) {
	if timezone == "" {
		timezone = "UTC"
	}
	if userID == "" || timezone == "Local" || len(timezone) > 100 {
		return Summary{}, ErrInvalid
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return Summary{}, ErrInvalid
	}
	now := s.now().In(location)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location)
	// A fixed calendar-day window keeps leap years and daylight saving transitions exact.
	window := Window{From: today.AddDate(0, 0, -365), To: today.AddDate(0, 0, 1), Timezone: timezone}
	result, err := s.repository.Summary(ctx, userID, window)
	if err != nil {
		return Summary{}, err
	}
	byDate := make(map[string]Day, len(result.Days))
	for _, day := range result.Days {
		byDate[day.Date] = day
	}
	result.Days = make([]Day, 0, 366)
	for date := window.From; date.Before(window.To); date = date.AddDate(0, 0, 1) {
		key := date.Format("2006-01-02")
		day := byDate[key]
		day.Date = key
		day.Level = Intensity(day.SpeakingMilliseconds + day.ReviewMilliseconds)
		result.Days = append(result.Days, day)
	}
	result.Timezone = timezone
	result.From = window.From.Format("2006-01-02")
	result.To = today.Format("2006-01-02")
	return result, nil
}

func Intensity(milliseconds int64) int {
	switch {
	case milliseconds <= 0:
		return 0
	case milliseconds <= 5*60*1000:
		return 1
	case milliseconds < 15*60*1000:
		return 2
	default:
		return 3
	}
}

// uncovered prevents overlapping screens, tabs, devices and retried batches from
// counting the same wall time twice. Speaking takes priority within a client;
// concurrent device intervals retain whichever category was accepted first.
func uncovered(input Interval, existing []Interval) []Interval {
	sort.Slice(existing, func(i, j int) bool { return existing[i].StartedAt.Before(existing[j].StartedAt) })
	cursor := input.StartedAt
	result := []Interval{}
	for _, prior := range existing {
		if !prior.EndedAt.After(cursor) || !prior.StartedAt.Before(input.EndedAt) {
			continue
		}
		if prior.StartedAt.After(cursor) {
			piece := input
			piece.StartedAt, piece.EndedAt = cursor, prior.StartedAt
			result = append(result, piece)
		}
		if prior.EndedAt.After(cursor) {
			cursor = prior.EndedAt
		}
		if !cursor.Before(input.EndedAt) {
			return result
		}
	}
	if cursor.Before(input.EndedAt) {
		piece := input
		piece.StartedAt = cursor
		result = append(result, piece)
	}
	return result
}
