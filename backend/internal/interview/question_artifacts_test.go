package interview

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"daily-speaking-practice/backend/internal/media"
	"daily-speaking-practice/backend/internal/storage"
	"daily-speaking-practice/backend/internal/workqueue"
)

type questionArtifactTestRepository struct {
	artifact  QuestionArtifact
	completed int
	created   bool
}

func (r *questionArtifactTestRepository) QuestionArtifact(_ context.Context, _ QuestionAudioInput, create bool) (QuestionArtifact, error) {
	r.created = r.created || create
	return r.artifact, nil
}
func (r *questionArtifactTestRepository) RetryQuestionAudio(context.Context, QuestionAudioInput, string) error {
	return nil
}

func (r *questionArtifactTestRepository) LoadQuestionAudio(context.Context, workqueue.Job) (QuestionArtifact, bool, error) {
	return r.artifact, true, nil
}
func (r *questionArtifactTestRepository) CompleteQuestionAudio(_ context.Context, _ workqueue.Job, audio, manifest media.Asset) error {
	if audio.Purpose != media.PurposeInterviewQuestionAudio || manifest.Purpose != "interview_question_manifest" {
		panic("incorrect purposes")
	}
	r.completed++
	return nil
}

type questionArtifactTestSynthesizer struct{ calls int }

func (s *questionArtifactTestSynthesizer) Synthesize(context.Context, string) ([]byte, error) {
	s.calls++
	return []byte("test mp3"), nil
}
func TestQuestionArtifactsReuseImmutableAudioAfterPublicationRetry(t *testing.T) {
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	objects := media.NewGeneratedStore(store, "", nil)
	repo := &questionArtifactTestRepository{artifact: QuestionArtifact{SessionID: "session", OwnerID: "guest", Index: 2, Question: "What do you like?"}}
	synth := &questionArtifactTestSynthesizer{}
	processor := NewQuestionAudioProcessor(repo, synth, objects)
	for i := 0; i < 2; i++ {
		if err := processor.Process(context.Background(), workqueue.Job{}); err != nil {
			t.Fatal(err)
		}
	}
	if synth.calls != 1 || repo.completed != 2 {
		t.Fatalf("tts=%d published=%d", synth.calls, repo.completed)
	}
	object, _, err := store.Open(context.Background(), "sessions/session/questions/2.json")
	if err != nil {
		t.Fatal(err)
	}
	defer object.Close()
	var manifest map[string]any
	if err := json.NewDecoder(object).Decode(&manifest); err != nil {
		t.Fatal(err)
	}
	if manifest["question"] != repo.artifact.Question || manifest["questionIndex"] != float64(2) || manifest["audioSha256"] == "" || manifest["speech"] == nil {
		t.Fatalf("manifest=%+v", manifest)
	}
}

type questionDownloadTestSigner struct{ err error }

func (s questionDownloadTestSigner) QuestionDownload(context.Context, media.DownloadInput) (media.Download, error) {
	return media.Download{}, s.err
}

func TestQuestionAudioMapsStorageErrorsWithoutStartingGeneration(t *testing.T) {
	for _, tc := range []struct{ storageError, want error }{
		{media.ErrNotFound, ErrNotFound},
		{media.ErrStorage, ErrSpeechUnavailable},
	} {
		repo := &questionArtifactTestRepository{artifact: QuestionArtifact{Status: "ready", AssetID: "audio"}}
		service := NewQuestionAudioService(repo, questionDownloadTestSigner{err: tc.storageError})
		_, err := service.Audio(context.Background(), QuestionAudioInput{OwnerID: "owner", SessionID: "session", Index: 1}, false)
		if !errors.Is(err, tc.want) || repo.created {
			t.Fatalf("error=%v, want=%v, started=%v", err, tc.want, repo.created)
		}
	}
}
