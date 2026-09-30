package recording

import (
	"context"
	"errors"

	"daily-speaking-practice/backend/internal/media"
	"daily-speaking-practice/backend/internal/workqueue"
)

var ErrFeedbackUnavailable = errors.New("feedback audio is unavailable")

type FeedbackAudioInput struct{ OwnerID, RecordingID, AttemptID, FeedbackID string }
type FeedbackAudioState struct {
	Status   string          `json:"status"`
	Error    string          `json:"error,omitempty"`
	AssetID  string          `json:"-"`
	Download *media.Download `json:"-"`
}

type FeedbackAudioRepository interface {
	FindFocus(context.Context, FeedbackAudioInput) (FeedbackFocus, error)
	AudioState(context.Context, FeedbackAudioInput) (FeedbackAudioState, error)
	ScheduleAudio(context.Context, FeedbackAudioInput, string) error
}

type AudioDownloadSigner interface {
	Download(context.Context, media.DownloadInput) (media.Download, error)
}

type FeedbackAudioService struct {
	repository FeedbackAudioRepository
	signer     AudioDownloadSigner
}

func NewFeedbackAudioService(repository FeedbackAudioRepository, signer AudioDownloadSigner) *FeedbackAudioService {
	return &FeedbackAudioService{repository: repository, signer: signer}
}

func (s *FeedbackAudioService) Audio(ctx context.Context, input FeedbackAudioInput, start bool) (FeedbackAudioState, error) {
	if s == nil || s.repository == nil {
		return FeedbackAudioState{}, ErrFeedbackUnavailable
	}
	focus, err := s.repository.FindFocus(ctx, input)
	if err != nil {
		return FeedbackAudioState{}, err
	}
	if focus.Kind == "praise" || focus.PracticeText == "" {
		return FeedbackAudioState{}, ErrFeedbackUnavailable
	}
	state, err := s.repository.AudioState(ctx, input)
	if err != nil {
		return state, err
	}
	if start && (state.Status == "not_requested" || state.Status == "failed") {
		if err := s.repository.ScheduleAudio(ctx, input, focus.PracticeText); err != nil {
			return state, err
		}
		state, err = s.repository.AudioState(ctx, input)
		if err != nil {
			return state, err
		}
	}
	if state.Status == "ready" && s.signer != nil {
		download, err := s.signer.Download(ctx, media.DownloadInput{OwnerPrincipalID: input.OwnerID, OwnerKind: "user", AssetID: state.AssetID})
		if err != nil {
			return state, err
		}
		state.Download = &download
	}
	return state, nil
}

type FeedbackAudioWork struct{ ID, OwnerID, PracticeText, RecordingID string }
type FeedbackAudioProcessingStore interface {
	LoadAudioWork(context.Context, workqueue.Job) (FeedbackAudioWork, bool, error)
	CompleteAudio(context.Context, workqueue.Job, media.Asset) error
}

type SpeechSynthesizer interface {
	Synthesize(context.Context, string) ([]byte, error)
}
type GeneratedMediaStore interface {
	Find(context.Context, string) (*media.Asset, error)
	Write(context.Context, string, string, string, string, []byte) (media.Asset, error)
}

type FeedbackAudioProcessor struct {
	repository  FeedbackAudioProcessingStore
	synthesizer SpeechSynthesizer
	objects     GeneratedMediaStore
}

func NewFeedbackAudioProcessor(repository FeedbackAudioProcessingStore, synthesizer SpeechSynthesizer, objects GeneratedMediaStore) *FeedbackAudioProcessor {
	return &FeedbackAudioProcessor{repository, synthesizer, objects}
}
func (p *FeedbackAudioProcessor) Process(ctx context.Context, job workqueue.Job) error {
	work, found, err := p.repository.LoadAudioWork(ctx, job)
	if err != nil || !found {
		return err
	}
	key := "recordings/" + work.RecordingID + "/feedback/" + work.ID + ".mp3"
	existing, err := p.objects.Find(ctx, key)
	if err != nil {
		return err
	}
	var asset media.Asset
	if existing != nil {
		asset = *existing
	} else {
		audio, err := p.synthesizer.Synthesize(ctx, work.PracticeText)
		if err != nil {
			return err
		}
		if len(audio) == 0 || len(audio) > 25*1024*1024 {
			return ErrFeedbackUnavailable
		}
		asset, err = p.objects.Write(ctx, work.OwnerID, "feedback_audio", key, "audio/mpeg", audio)
		if err != nil {
			return err
		}
	}
	return p.repository.CompleteAudio(ctx, job, asset)
}
