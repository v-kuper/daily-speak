package recording

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"daily-speaking-practice/backend/internal/aiparse"
)

func focusJSON(items []map[string]any, seq int) string {
	bytes, _ := json.Marshal(map[string]any{"answers": []any{map[string]any{"turnSequence": seq, "items": items}}})
	return string(bytes)
}
func testFocus(kind, fragment string, occurrence int) map[string]any {
	item := map[string]any{"kind": kind, "original_fragment": fragment, "occurrence": occurrence, "title": "Вы хотели рассказать о прошлом", "explanation": "Для прошедшего действия используйте прошедшую форму.", "ruleId": "verb-forms", "category": "verb_grammar"}
	if kind != "praise" {
		item["corrected_fragment"] = "liked"
		item["practiceText"] = "I liked it yesterday."
	}
	return item
}
func TestFocusedOccurrenceIsOneBasedAndUTF16(t *testing.T) {
	input := AnalysisInput{Transcript: "😀 I like it, unlike before, I like it."}
	feedback, valid := parseFocusedFeedback(focusJSON([]map[string]any{testFocus("blocker", "like", 2)}, 0), input)
	if !valid || feedback.Answers[0].Items[0].Span.Start != 31 || feedback.Answers[0].Items[0].Span.End != 35 {
		t.Fatalf("feedback=%+v valid=%v", feedback, valid)
	}
	for _, occurrence := range []int{0, -1, 3} {
		if _, valid := parseFocusedFeedback(focusJSON([]map[string]any{testFocus("blocker", "like", occurrence)}, 0), input); valid {
			t.Fatalf("accepted occurrence %d", occurrence)
		}
	}
	missing := testFocus("blocker", "like", 1)
	delete(missing, "occurrence")
	if _, valid := parseFocusedFeedback(focusJSON([]map[string]any{missing}, 0), input); valid {
		t.Fatal("accepted missing occurrence")
	}
}
func TestFocusedQuotesCannotSelectQuestionsOrOtherAnswers(t *testing.T) {
	input := AnalysisInput{Transcript: "I like it", InterviewTurns: []InterviewDialogueTurn{{Sequence: 2, Question: "Do you like it?", Answer: "Yes."}}}
	if _, valid := parseFocusedFeedback(focusJSON([]map[string]any{testFocus("blocker", "like", 1)}, 2), input); valid {
		t.Fatal("accepted question quote")
	}
	input.InterviewTurns = append(input.InterviewTurns, InterviewDialogueTurn{Sequence: 5, Question: "Why?", Answer: "I like it."})
	if _, valid := parseFocusedFeedback(focusJSON([]map[string]any{testFocus("blocker", "like", 1)}, 2), input); valid {
		t.Fatal("accepted another answer")
	}
}

func TestFocusedPromptUsesTheInterviewSequenceAndRequiresPraiseRules(t *testing.T) {
	input := AnalysisInput{Transcript: "I like it.", InterviewTurns: []InterviewDialogueTurn{{Sequence: 7, Answer: "I like it."}}}
	candidates := aiparse.ExtractJSONCandidates(focusedPrompt(input))
	var example struct {
		Answers []focusedWireAnswer `json:"answers"`
	}
	for _, candidate := range candidates {
		if json.Unmarshal([]byte(candidate), &example) == nil && example.Answers != nil {
			break
		}
	}
	if len(example.Answers) != 1 || example.Answers[0].TurnSequence != 7 {
		t.Fatal("output example must use the supplied interview sequence rather than a generic zero")
	}
	if !strings.Contains(focusedPrompt(input), "INCLUDING PRAISE") || !strings.Contains(focusedPrompt(input), "For praise omit ONLY corrected_fragment and practiceText") {
		t.Fatal("praise must retain the mandatory ruleId and category fields")
	}
	praise := testFocus("praise", "like", 1)
	if _, valid := parseFocusedFeedback(focusJSON([]map[string]any{praise}, 7), input); !valid {
		t.Fatal("valid praise in the supplied turn was rejected")
	}
	if _, valid := parseFocusedFeedback(focusJSON([]map[string]any{praise}, 0), input); valid {
		t.Fatal("generic example sequence must not replace the actual interview sequence")
	}
	delete(praise, "ruleId")
	if _, valid := parseFocusedFeedback(focusJSON([]map[string]any{praise}, 7), input); valid {
		t.Fatal("praise without a curated rule must not pass validation")
	}
}
func TestFocusedBudgetAndOverlapPriority(t *testing.T) {
	input := AnalysisInput{Transcript: "I like it."}
	praise := testFocus("praise", "like", 1)
	blocker := testFocus("blocker", "like", 1)
	tip := testFocus("native_tip", "like", 1)
	feedback, valid := parseFocusedFeedback(focusJSON([]map[string]any{tip, praise, blocker}, 0), input)
	if !valid || len(feedback.Answers[0].Items) != 1 || feedback.Answers[0].Items[0].Kind != "blocker" {
		t.Fatalf("overlap=%+v valid=%v", feedback, valid)
	}
	for _, items := range [][]map[string]any{{praise, praise}, {tip, tip}, {praise, blocker, tip, praise}} {
		if _, valid := parseFocusedFeedback(focusJSON(items, 0), input); valid {
			t.Fatalf("accepted excess budget %+v", items)
		}
	}
}
func TestFocusedRussianFillersShareBudget(t *testing.T) {
	item := testFocus("native_tip", "ну как это", 1)
	item["ruleId"] = "english-fillers"
	item["category"] = "language_switch"
	item["corrected_fragment"] = "let me think"
	item["practiceText"] = "I want to, let me think, apply for the job."
	feedback, valid := parseFocusedFeedback(focusJSON([]map[string]any{item}, 0), AnalysisInput{Transcript: "I want to, ну как это, apply for the job."})
	if !valid || feedback.Answers[0].Items[0].MicroLesson == nil {
		t.Fatalf("feedback=%+v", feedback)
	}
}

func TestFocusedThreeErrorsHaveIndependentPraiseAndNativeTipBudgets(t *testing.T) {
	input := AnalysisInput{Transcript: "I go and make and take notes. I enjoy my career grow."}
	items := []map[string]any{testFocus("blocker", "go", 1), testFocus("blocker", "make", 1), testFocus("blocker", "take", 1), testFocus("praise", "enjoy", 1), testFocus("native_tip", "career grow", 1)}
	feedback, valid := parseFocusedFeedback(focusJSON(items, 0), input)
	if !valid || len(feedback.Answers[0].Items) != 5 {
		t.Fatalf("feedback=%+v valid=%v", feedback, valid)
	}
	items = append(items[:3], testFocus("blocker", "notes", 1))
	if feedback, valid := parseFocusedFeedback(focusJSON(items, 0), input); !valid || len(feedback.Answers[0].Items) != 4 {
		t.Fatal("four verified errors must pass without a count limit")
	}
}

type focusedTestProvider struct {
	calls   int
	payload string
}

func (p *focusedTestProvider) Complete(_ context.Context, r AnalysisCompletionRequest) (string, error) {
	p.calls++
	if !r.StrictJSON || !strings.Contains(r.SystemPrompt, "1-based") {
		panic("invalid focused prompt")
	}
	return p.payload, nil
}

type focusedTestCheckpoint struct{ feedback *FocusedFeedback }

func (c *focusedTestCheckpoint) LoadFocused(context.Context) (*FocusedFeedback, bool, error) {
	return c.feedback, c.feedback != nil, nil
}
func (c *focusedTestCheckpoint) SaveFocused(_ context.Context, f *FocusedFeedback) error {
	c.feedback = f
	return nil
}
func TestFocusedPipelineUsesOneCallAndReusesCheckpoint(t *testing.T) {
	provider := &focusedTestProvider{payload: focusJSON([]map[string]any{testFocus("blocker", "like", 1)}, 0)}
	checkpoint := &focusedTestCheckpoint{}
	input := AnalysisInput{Pipeline: FocusedPipeline, Transcript: "I like it yesterday.", FocusedCheckpoint: checkpoint}
	service := NewAnalysisService(provider, AnalysisConfig{})
	for i := 0; i < 2; i++ {
		result, err := service.Analyze(context.Background(), input, nil)
		if err != nil || result.FocusedFeedback == nil || len(result.Suggestions) != 1 || result.StrengthsStatus != "ready" {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	}
	if provider.calls != 1 {
		t.Fatalf("calls=%d", provider.calls)
	}
}
func TestEveryReferenceHasCuratedMicroLesson(t *testing.T) {
	for id := range learningReferenceCatalog {
		lesson := LessonFor(id)
		if lesson == nil {
			t.Errorf("missing lesson for %s", id)
			continue
		}
		for _, point := range lesson.Points {
			if !containsCyrillic(point) {
				t.Errorf("invalid lesson %s", id)
			}
		}
	}
}

func TestLegacyCheckpointFingerprintSurvivesFocusedRollout(t *testing.T) {
	input := AnalysisInput{Pipeline: "legacy-v2", RecordingID: "r", Transcript: "I go."}
	checkpoint := NewSQLProcessingRepository(nil).AnalysisCheckpoint(ProcessingJob{}, input).(*sqlAnalysisCheckpoint)
	data := `{"RecordingID":"r","Transcript":"I go.","InterviewTurns":null,"Topic":"","Interests":null,"PracticeType":"","PhotoObject":null,"EnglishLevel":"","Checkpoint":null}`
	hash := sha256.Sum256([]byte("feedback-v2:" + data))
	if checkpoint.fingerprint != fmt.Sprintf("%x", hash) {
		t.Fatalf("legacy fingerprint changed: %s", checkpoint.fingerprint)
	}
}
func TestFocusedFeedbackKeepsAllTwentyFiveVerifiedErrors(t *testing.T) {
	input := AnalysisInput{Transcript: strings.Repeat("I go yesterday. ", 25)}
	items := []map[string]any{}
	for occurrence := 1; occurrence <= 25; occurrence++ {
		item := testFocus("blocker", "go", occurrence)
		item["corrected_fragment"] = "went"
		items = append(items, item)
	}
	feedback, valid := parseFocusedFeedback(focusJSON(items, 0), input)
	if !valid || len(feedback.Answers[0].Items) != 25 || len(focusedResult(feedback).Suggestions) != 25 {
		t.Fatalf("all errors lost: valid=%v feedback=%#v", valid, feedback)
	}
	for i, item := range feedback.Answers[0].Items {
		if item.Occurrence != i+1 || item.Span.Start != i*16+2 {
			t.Fatalf("wrong occurrence anchor: %#v", item)
		}
	}
	if !strings.Contains(focusedSystemPrompt, "no numeric limit") || !strings.Contains(focusedSystemPrompt, "independent example") {
		t.Fatal("analysis still limits errors or rewrites practice examples")
	}
}
