package recording

import (
	"context"
	"errors"
	"strings"
	"time"
)

var ErrRetryUnavailable = errors.New("This recording cannot be retried from its current stage.")

type RetryTransaction interface {
	Claim(context.Context, string, string, string) (bool, error)
	Find(context.Context, string, string) (Record, bool, error)
	Enqueue(context.Context, string, string) error
}

type RetryUnitOfWork interface {
	Execute(context.Context, func(RetryTransaction) error) error
}

type RetryResult struct {
	Record    Record
	Scheduled bool
}

type RetryService struct {
	records RecordRepository
	unit    RetryUnitOfWork
	newID   func() string
	now     func() time.Time
}

func NewRetryService(records RecordRepository, unit RetryUnitOfWork, newID func() string) *RetryService {
	return &RetryService{records: records, unit: unit, newID: newID, now: time.Now}
}

func (service *RetryService) Retry(ctx context.Context, userID string, recordingID string) (RetryResult, error) {
	if service == nil || service.records == nil || service.unit == nil || service.newID == nil || service.now == nil {
		return RetryResult{}, errors.New("recording retry service is not configured")
	}
	recordingID = strings.TrimSpace(recordingID)
	if recordingID == "" {
		return RetryResult{}, ErrNotFound
	}
	record, found, err := service.records.Find(ctx, userID, recordingID)
	if err != nil {
		return RetryResult{}, err
	}
	if !found {
		return RetryResult{}, ErrNotFound
	}
	if record.Status == "processing" {
		return RetryResult{Record: record}, nil
	}
	if err := service.validate(record); err != nil {
		return RetryResult{}, err
	}

	jobID := strings.TrimSpace(service.newID())
	if jobID == "" {
		return RetryResult{}, errors.New("recording retry job ID is not configured")
	}
	startedAt := service.now().UTC()
	result := RetryResult{}
	err = service.unit.Execute(ctx, func(tx RetryTransaction) error {
		claimed, err := tx.Claim(ctx, userID, recordingID, jobID)
		if err != nil {
			return err
		}
		if !claimed {
			current, found, err := tx.Find(ctx, userID, recordingID)
			if err != nil {
				return err
			}
			if !found {
				return ErrNotFound
			}
			if current.Status == "processing" {
				result.Record = current
				return nil
			}
			return ErrRetryUnavailable
		}
		if err := tx.Enqueue(ctx, jobID, recordingID); err != nil {
			return err
		}
		result = RetryResult{Record: afterRetryClaim(record, startedAt), Scheduled: true}
		return nil
	})
	if err != nil {
		return RetryResult{}, err
	}
	return result, nil
}

func (service *RetryService) validate(record Record) error {
	if record.Status != "failed" || record.ProcessingStage == nil {
		return ErrRetryUnavailable
	}
	switch *record.ProcessingStage {
	case "transcribing":
		if record.AudioAssetID == nil || strings.TrimSpace(*record.AudioAssetID) == "" {
			return ErrRetryUnavailable
		}
	case "suggestions", "rewriting":
		if strings.TrimSpace(record.Transcript) == "" {
			return ErrRetryUnavailable
		}
	default:
		return ErrRetryUnavailable
	}
	return nil
}

func afterRetryClaim(record Record, startedAt time.Time) Record {
	record.Status = "processing"
	record.ProcessingError = nil
	record.CorrectedTranscript = ""
	record.ShadowingStatus = "pending"
	record.ShadowingAssetID = nil
	record.ShadowingError = nil
	record.ShadowingUpdatedAt = startedAt.UTC()
	for index := range record.InterviewTurns {
		record.InterviewTurns[index].CorrectedAnswerText = ""
	}
	if record.ProcessingStage != nil {
		switch *record.ProcessingStage {
		case "transcribing":
			record.Transcript = ""
			record.SuggestionsJSON = []byte("[]")
			record.StrengthsJSON = []byte("[]")
			record.StrengthsStatus = "pending"
		case "suggestions":
			record.SuggestionsJSON = []byte("[]")
			record.StrengthsJSON = []byte("[]")
			record.StrengthsStatus = "pending"
		}
	}
	return record
}
