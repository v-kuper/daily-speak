package recording

import (
	"context"
	"errors"
	"strings"
)

var ErrNotFound = errors.New("recording not found")

type RecordRepository interface {
	Find(context.Context, string, string) (Record, bool, error)
}

type Reader struct {
	repository RecordRepository
}

func NewReader(repository RecordRepository) *Reader {
	return &Reader{repository: repository}
}

func (reader *Reader) Get(ctx context.Context, userID string, recordingID string) (Record, error) {
	if reader == nil || reader.repository == nil {
		return Record{}, errors.New("recording reader is not configured")
	}
	recordingID = strings.TrimSpace(recordingID)
	if recordingID == "" {
		return Record{}, ErrNotFound
	}
	record, found, err := reader.repository.Find(ctx, userID, recordingID)
	if err != nil {
		return Record{}, err
	}
	if !found {
		return Record{}, ErrNotFound
	}
	return record, nil
}
