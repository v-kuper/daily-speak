package recording

import (
	"context"
	"errors"
	"strings"

	"daily-speaking-practice/backend/internal/quota"
)

var ErrDeleteNotFound = errors.New("recording not found")

type DeletionSource struct {
	LegacyURLs []*string
	AssetIDs   []*string
}

type DeletionTransaction interface {
	Load(context.Context, string, string) (DeletionSource, bool, error)
	QueueLegacyMedia(context.Context, string, string) error
	QueueAsset(context.Context, string, string) error
	Remove(context.Context, string, string) (bool, error)
}

type DeletionUnitOfWork interface {
	Execute(context.Context, func(DeletionTransaction) error) error
}

type LegacyUploadValidator interface {
	Path(string) (string, error)
}

type DeletionQuotaReader interface {
	Get(context.Context, string, bool) (quota.RecordingQuota, error)
}

type DeletionResult struct {
	RecordingID        string
	Quota              *quota.RecordingQuota
	QuotaRefreshFailed bool
}

type Deleter struct {
	unitOfWork DeletionUnitOfWork
	legacy     LegacyUploadValidator
	quota      DeletionQuotaReader
	newID      func() string
}

func NewDeleter(unitOfWork DeletionUnitOfWork, legacy LegacyUploadValidator, quotaReader DeletionQuotaReader, newID func() string) *Deleter {
	return &Deleter{unitOfWork: unitOfWork, legacy: legacy, quota: quotaReader, newID: newID}
}

func (d *Deleter) Delete(ctx context.Context, userID string, subscriber bool, recordingID string) (DeletionResult, error) {
	if d == nil || d.unitOfWork == nil || d.newID == nil {
		return DeletionResult{}, errors.New("recording deleter is not configured")
	}
	recordingID = strings.TrimSpace(recordingID)
	if recordingID == "" {
		return DeletionResult{}, ErrDeleteNotFound
	}
	err := d.unitOfWork.Execute(ctx, func(tx DeletionTransaction) error {
		source, found, err := tx.Load(ctx, userID, recordingID)
		if err != nil {
			return err
		}
		if !found {
			return ErrDeleteNotFound
		}
		for _, publicURL := range d.uniqueLegacyURLs(source.LegacyURLs) {
			if err := tx.QueueLegacyMedia(ctx, publicURL, d.newID()); err != nil {
				return err
			}
		}
		for _, assetID := range uniqueDeletionValues(source.AssetIDs) {
			if err := tx.QueueAsset(ctx, assetID, d.newID()); err != nil {
				return err
			}
		}
		removed, err := tx.Remove(ctx, userID, recordingID)
		if err != nil {
			return err
		}
		if !removed {
			return ErrDeleteNotFound
		}
		return nil
	})
	if err != nil {
		return DeletionResult{}, err
	}
	result := DeletionResult{RecordingID: recordingID}
	if d.quota == nil {
		return result, nil
	}
	current, err := d.quota.Get(ctx, userID, subscriber)
	if err != nil {
		result.QuotaRefreshFailed = true
		return result, nil
	}
	result.Quota = &current
	return result, nil
}

func (d *Deleter) uniqueLegacyURLs(values []*string) []string {
	unique := uniqueDeletionValues(values)
	if d.legacy == nil {
		return nil
	}
	valid := make([]string, 0, len(unique))
	for _, value := range unique {
		if _, err := d.legacy.Path(value); err == nil {
			valid = append(valid, value)
		}
	}
	return valid
}

func uniqueDeletionValues(values []*string) []string {
	seen := map[string]struct{}{}
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		normalized := strings.TrimSpace(*value)
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		unique = append(unique, normalized)
	}
	return unique
}
