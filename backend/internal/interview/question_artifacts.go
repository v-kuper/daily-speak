package interview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"daily-speaking-practice/backend/internal/media"
	"daily-speaking-practice/backend/internal/workqueue"
)

type QuestionAudioInput struct {
	OwnerID, SessionID, RecordingID string
	Index, TurnSeq                  int
}
type QuestionArtifact struct {
	ID, SessionID, OwnerID, Question, Status, AssetID, Error string
	Index                                                    int
}
type QuestionAudioResult struct {
	Status        string          `json:"status"`
	QuestionIndex int             `json:"questionIndex,omitempty"`
	Error         string          `json:"error,omitempty"`
	Download      *media.Download `json:"-"`
}
type QuestionArtifactRepository interface {
	QuestionArtifact(context.Context, QuestionAudioInput, bool) (QuestionArtifact, error)
	RetryQuestionAudio(context.Context, QuestionAudioInput, string) error
}
type QuestionDownloadSigner interface {
	QuestionDownload(context.Context, media.DownloadInput) (media.Download, error)
}
type QuestionAudioService struct {
	repository QuestionArtifactRepository
	signer     QuestionDownloadSigner
}

func NewQuestionAudioService(repository QuestionArtifactRepository, signer QuestionDownloadSigner) *QuestionAudioService {
	return &QuestionAudioService{repository, signer}
}
func (s *QuestionAudioService) Audio(ctx context.Context, input QuestionAudioInput, start bool) (QuestionAudioResult, error) {
	if s == nil || s.repository == nil {
		return QuestionAudioResult{}, ErrSpeechUnavailable
	}
	artifact, err := s.repository.QuestionArtifact(ctx, input, start)
	if err != nil {
		return QuestionAudioResult{}, err
	}
	if start && artifact.Status == "failed" {
		if err := s.repository.RetryQuestionAudio(ctx, input, artifact.ID); err != nil {
			return QuestionAudioResult{}, err
		}
		artifact, err = s.repository.QuestionArtifact(ctx, input, false)
		if err != nil {
			return QuestionAudioResult{}, err
		}
	}
	result := QuestionAudioResult{Status: artifact.Status, QuestionIndex: artifact.Index, Error: artifact.Error}
	if result.Status == "ready" && s.signer != nil {
		download, err := s.signer.QuestionDownload(ctx, media.DownloadInput{OwnerPrincipalID: input.OwnerID, AssetID: artifact.AssetID})
		if err != nil {
			if errors.Is(err, media.ErrNotFound) {
				return result, ErrNotFound
			}
			return result, fmt.Errorf("%w: %v", ErrSpeechUnavailable, err)
		}
		result.Download = &download
	}
	return result, nil
}

type QuestionAudioProcessingStore interface {
	LoadQuestionAudio(context.Context, workqueue.Job) (QuestionArtifact, bool, error)
	CompleteQuestionAudio(context.Context, workqueue.Job, media.Asset, media.Asset) error
}
type QuestionSynthesizer interface {
	Synthesize(context.Context, string) ([]byte, error)
}
type QuestionObjects interface {
	Find(context.Context, string) (*media.Asset, error)
	Write(context.Context, string, string, string, string, []byte) (media.Asset, error)
}
type QuestionAudioProcessor struct {
	repository  QuestionAudioProcessingStore
	synthesizer QuestionSynthesizer
	objects     QuestionObjects
}

func NewQuestionAudioProcessor(repository QuestionAudioProcessingStore, synthesizer QuestionSynthesizer, objects QuestionObjects) *QuestionAudioProcessor {
	return &QuestionAudioProcessor{repository, synthesizer, objects}
}
func (p *QuestionAudioProcessor) Process(ctx context.Context, job workqueue.Job) error {
	artifact, found, err := p.repository.LoadQuestionAudio(ctx, job)
	if err != nil || !found {
		return err
	}
	key := fmt.Sprintf("sessions/%s/questions/%d", artifact.SessionID, artifact.Index)
	existing, err := p.objects.Find(ctx, key+".mp3")
	if err != nil {
		return err
	}
	var audio media.Asset
	if existing != nil {
		audio = *existing
	} else {
		bytes, err := p.synthesizer.Synthesize(ctx, artifact.Question)
		if err != nil {
			return err
		}
		if len(bytes) == 0 || len(bytes) > 25*1024*1024 {
			return ErrSpeechUnavailable
		}
		audio, err = p.objects.Write(ctx, artifact.OwnerID, media.PurposeInterviewQuestionAudio, key+".mp3", "audio/mpeg", bytes)
		if err != nil {
			return err
		}
	}
	existing, err = p.objects.Find(ctx, key+".json")
	if err != nil {
		return err
	}
	var manifest media.Asset
	if existing != nil {
		manifest = *existing
	} else {
		bytes, _ := json.Marshal(map[string]any{"version": 1, "sessionId": artifact.SessionID, "questionIndex": artifact.Index, "question": artifact.Question, "audioKey": key + ".mp3", "audioSha256": audio.ExpectedChecksumSHA256, "speech": questionSpeechMetadata(p.synthesizer)})
		manifest, err = p.objects.Write(ctx, artifact.OwnerID, "interview_question_manifest", key+".json", "application/json", bytes)
		if err != nil {
			return err
		}
	}
	return p.repository.CompleteQuestionAudio(ctx, job, audio, manifest)
}

func questionSpeechMetadata(synthesizer QuestionSynthesizer) map[string]any {
	if provider, ok := synthesizer.(interface{ SpeechMetadata() map[string]any }); ok {
		return provider.SpeechMetadata()
	}
	return map[string]any{"container": "mp3", "language": "en"}
}
