package shadowing

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/storage"
)

type Synthesizer interface {
	Synthesize(context.Context, string) ([]byte, error)
}

type Logger interface{ Info(string, map[string]any) }

type ProcessingStore interface {
	LoadWork(context.Context, Job) (Work, bool, error)
	Complete(context.Context, Job, Asset) (bool, error)
}

type ProcessorDependencies struct {
	Store       ProcessingStore
	Synthesizer Synthesizer
	MediaStore  storage.Store
	MediaBucket string
	LocalSaver  *LocalSaver
	NewID       func() string
}

type Processor struct{ dependencies ProcessorDependencies }

func NewProcessor(dependencies ProcessorDependencies) *Processor {
	return &Processor{dependencies: dependencies}
}

func (p *Processor) Process(ctx context.Context, job Job, logger Logger) error {
	if p == nil || p.dependencies.Store == nil {
		return errors.New("shadowing processor is not configured")
	}
	started := time.Now()
	work, found, err := p.dependencies.Store.LoadWork(ctx, job)
	if err != nil || !found {
		return err
	}
	if p.dependencies.Synthesizer == nil {
		return errors.New("shadowing synthesizer is not configured")
	}
	audio, err := p.dependencies.Synthesizer.Synthesize(ctx, work.CorrectedTranscript)
	if err != nil {
		return err
	}
	if len(audio) == 0 || len(audio) > MaxAudioBytes {
		return errors.New("shadowing audio payload is invalid")
	}
	assetID := ""
	if p.dependencies.NewID != nil {
		assetID = p.dependencies.NewID()
	}
	if assetID == "" {
		return errors.New("shadowing ID generator is not configured")
	}
	checksumBytes := sha256.Sum256(audio)
	checksum := hex.EncodeToString(checksumBytes[:])
	asset := Asset{ID: assetID, OwnerID: work.UserID, Checksum: checksum}
	if p.dependencies.MediaStore == nil {
		return errors.New("media storage is not configured")
	}
	asset.StorageDriver = p.dependencies.MediaStore.Backend()
	if asset.StorageDriver == storage.BackendLocal {
		if p.dependencies.LocalSaver == nil {
			return errors.New("local shadowing storage is not configured")
		}
		saved, saveErr := p.dependencies.LocalSaver.Save(work.UserID, job.ResourceID, job.ID+"-"+job.LeaseToken, audio)
		if saveErr != nil {
			return saveErr
		}
		asset.LegacyPublicURL = saved.PublicURL
		asset.ObjectKey = strings.TrimPrefix(saved.PublicURL, "/uploads/")
		info, statErr := p.dependencies.MediaStore.Stat(ctx, asset.ObjectKey)
		if statErr != nil {
			_ = os.Remove(saved.AbsolutePath)
			return statErr
		}
		asset.Size, asset.Checksum, asset.ETag = info.Size, info.SHA256, info.ETag
	} else {
		asset.Bucket = strings.TrimSpace(p.dependencies.MediaBucket)
		asset.ObjectKey, err = storage.NewObjectKey(work.UserID, "shadowing_audio", "mp3")
		if err == nil {
			var info storage.ObjectInfo
			info, err = p.dependencies.MediaStore.Put(ctx, storage.PutRequest{
				Key: asset.ObjectKey, ContentType: "audio/mpeg", Size: int64(len(audio)), SHA256: checksum,
				Metadata: map[string]string{"asset-id": assetID, "owner-principal-id": work.UserID, "purpose": "shadowing_audio"},
			}, bytes.NewReader(audio))
			asset.Size, asset.Checksum, asset.ETag = info.Size, info.SHA256, info.ETag
		}
	}
	if err != nil {
		return err
	}
	completed, err := p.dependencies.Store.Complete(ctx, job, asset)
	if err != nil || !completed {
		_ = p.dependencies.MediaStore.Delete(context.Background(), asset.ObjectKey)
		return err
	}
	logger.Info("shadowing.ready", map[string]any{"recordingId": job.ResourceID, "durationMs": time.Since(started).Milliseconds(), "bytes": len(audio)})
	return nil
}
