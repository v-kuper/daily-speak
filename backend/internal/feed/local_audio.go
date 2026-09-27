package feed

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"daily-speaking-practice/backend/internal/domain"
)

type LocalReplyAudioStore struct {
	root string
}

func NewLocalReplyAudioStore(root string) *LocalReplyAudioStore {
	return &LocalReplyAudioStore{root: filepath.Clean(root)}
}

func (store *LocalReplyAudioStore) Save(_ context.Context, ownerID string, replyID string, audio *domain.ParsedAudioDataURL) (string, func(), error) {
	if store == nil || store.root == "" || audio == nil {
		return "", nil, ErrReplyStorage
	}
	ownerDir := sanitizePathSegment(ownerID)
	fileName := sanitizePathSegment(replyID) + "." + audio.Extension
	directory := filepath.Join(store.root, "feed-replies", ownerDir)
	absolutePath := filepath.Join(directory, fileName)
	data, err := domain.DecodeBase64(audio.Base64)
	if err != nil || len(data) <= 0 || len(data) > domain.MaxAudioUploadBytes {
		return "", nil, errors.New("audio payload is invalid")
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", nil, err
	}
	if err := os.WriteFile(absolutePath, data, 0o644); err != nil {
		return "", nil, err
	}
	rollback := func() { _ = os.Remove(absolutePath) }
	return "/uploads/feed-replies/" + ownerDir + "/" + fileName, rollback, nil
}
