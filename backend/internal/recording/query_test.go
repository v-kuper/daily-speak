package recording

import (
	"context"
	"errors"
	"testing"
)

type recordRepositoryStub struct {
	record  Record
	records []Record
	found   bool
	err     error
}

func (repository *recordRepositoryStub) Find(context.Context, string, string) (Record, bool, error) {
	return repository.record, repository.found, repository.err
}

func (repository *recordRepositoryStub) List(context.Context, string, ListOptions) ([]Record, error) {
	return repository.records, repository.err
}

func TestReaderReturnsOwnedRecord(t *testing.T) {
	repository := &recordRepositoryStub{found: true, record: Record{ID: "recording-id", Topic: "Topic"}}
	reader := NewReader(repository)

	record, err := reader.Get(context.Background(), "user-id", " recording-id ")
	if err != nil {
		t.Fatalf("get recording: %v", err)
	}
	if record.ID != "recording-id" {
		t.Fatalf("unexpected record: %#v", record)
	}
}

func TestReaderHidesMissingAndEmptyIdentifiers(t *testing.T) {
	repository := &recordRepositoryStub{}
	reader := NewReader(repository)
	for _, recordingID := range []string{"", "missing"} {
		if _, err := reader.Get(context.Background(), "user-id", recordingID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("recording %q: expected not found, got %v", recordingID, err)
		}
	}
}
