package httpapi

import (
	"context"
	"strings"
	"testing"

	"daily-speaking-practice/backend/internal/ai"
	"daily-speaking-practice/backend/internal/quota"
)

type stubChatClient struct {
	post func(context.Context, any) (ai.ChatResponse, error)
}

func (s stubChatClient) PostChat(ctx context.Context, body any) (ai.ChatResponse, error) {
	return s.post(ctx, body)
}

func TestNewServerUsesInjectedAIClient(t *testing.T) {
	client := stubChatClient{post: func(context.Context, any) (ai.ChatResponse, error) {
		return ai.ChatResponse{}, nil
	}}
	server := NewServer(Config{AIClient: client})
	if server.aiClient == nil {
		t.Fatal("expected injected AI client")
	}
}

func TestParseNaturalTranscriptFromContentReadsCorrectedTranscript(t *testing.T) {
	got := parseNaturalTranscriptFromContent(`{"correctedTranscript":"I went to the store yesterday."}`)

	if got != "I went to the store yesterday." {
		t.Fatalf("expected corrected transcript, got %q", got)
	}
}

func TestParseNaturalTranscriptFromContentRejectsEmptyText(t *testing.T) {
	got := parseNaturalTranscriptFromContent(`{"correctedTranscript":"   "}`)

	if got != "" {
		t.Fatalf("expected empty corrected transcript to be rejected, got %q", got)
	}
}

func TestParseNaturalTranscriptFromContentRejectsRemainingRussianText(t *testing.T) {
	got := parseNaturalTranscriptFromContent(`{"correctedTranscript":"I bought капуста for dinner."}`)

	if got != "" {
		t.Fatalf("expected corrected transcript with Cyrillic to be rejected, got %q", got)
	}
}

func TestParseSuggestionsResultFromContentAcceptsValidEmptyList(t *testing.T) {
	got, valid := parseSuggestionsResultFromContent(`{"suggestions":[]}`)

	if !valid || len(got) != 0 {
		t.Fatalf("expected a valid empty suggestion list, got %#v, valid=%v", got, valid)
	}
}

func TestParseSuggestionsResultFromContentRejectsMalformedSuggestions(t *testing.T) {
	_, valid := parseSuggestionsResultFromContent(`{"suggestions":[{"wrong":"I go"}]}`)

	if valid {
		t.Fatal("expected an incomplete suggestion to be rejected")
	}
}

func TestParseSuggestionsResultFromContentKeepsMoreThanFiveCorrections(t *testing.T) {
	got, valid := parseSuggestionsResultFromContent(`{"suggestions":[
		{"wrong":"один","right":"one","explanation":"Use English."},
		{"wrong":"два","right":"two","explanation":"Use English."},
		{"wrong":"три","right":"three","explanation":"Use English."},
		{"wrong":"четыре","right":"four","explanation":"Use English."},
		{"wrong":"пять","right":"five","explanation":"Use English."},
		{"wrong":"шесть","right":"six","explanation":"Use English."}
	]}`)

	if !valid || len(got) != 6 {
		t.Fatalf("expected all six corrections, got %#v, valid=%v", got, valid)
	}
}

func TestExtractRussianPhrasesKeepsExactUniqueTranscriptText(t *testing.T) {
	got := extractRussianPhrases("I bought капуста, but я не знаю how to say it по-русски, капуста.")
	want := []string{"капуста", "я не знаю", "по-русски"}

	if len(got) != len(want) {
		t.Fatalf("expected %#v, got %#v", want, got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("expected %#v, got %#v", want, got)
		}
	}
}

func TestRussianSuggestionsCoveredRequiresExactEnglishCorrections(t *testing.T) {
	required := []string{"капуста", "я не знаю"}
	valid := []suggestion{
		{Wrong: "капуста", Right: "cabbage", Explanation: "Use the English word."},
		{Wrong: "я не знаю", Right: "I don't know", Explanation: "Use the English phrase."},
	}

	if !russianSuggestionsCovered(valid, required) {
		t.Fatal("expected exact English corrections to cover all Russian phrases")
	}
	if russianSuggestionsCovered(valid[:1], required) {
		t.Fatal("expected a missing Russian phrase to be rejected")
	}
	invalidTranslation := append([]suggestion{}, valid...)
	invalidTranslation[0].Right = "cabbage капуста"
	if russianSuggestionsCovered(invalidTranslation, required) {
		t.Fatal("expected a correction that still contains Cyrillic to be rejected")
	}
}

func TestSelectRecordingSuggestionsKeepsAllRussianAndEnglishCorrections(t *testing.T) {
	required := []string{"один", "два", "три", "четыре", "пять", "шесть"}
	items := []suggestion{
		{Wrong: "один", Right: "one", Explanation: "Use English."},
		{Wrong: "два", Right: "two", Explanation: "Use English."},
		{Wrong: "три", Right: "three", Explanation: "Use English."},
		{Wrong: "четыре", Right: "four", Explanation: "Use English."},
		{Wrong: "пять", Right: "five", Explanation: "Use English."},
		{Wrong: "шесть", Right: "six", Explanation: "Use English."},
		{Wrong: "I go", Right: "I went", Explanation: "Use past tense."},
		{Wrong: "a apple", Right: "an apple", Explanation: "Use an."},
		{Wrong: "she have", Right: "she has", Explanation: "Use has."},
		{Wrong: "in Monday", Right: "on Monday", Explanation: "Use on."},
		{Wrong: "he do", Right: "he does", Explanation: "Use does."},
	}

	got := selectRecordingSuggestions(items, required)

	if len(got) != 11 {
		t.Fatalf("expected every Russian and English correction, got %#v", got)
	}
	if got[10].Wrong != "he do" {
		t.Fatalf("expected the final English correction to be kept, got %#v", got)
	}
}

func TestSelectRecordingSuggestionsSkipsInvalidDuplicateRussianTranslation(t *testing.T) {
	items := []suggestion{
		{Wrong: "капуста", Right: "капуста", Explanation: "Invalid translation."},
		{Wrong: "капуста", Right: "cabbage", Explanation: "Use the English word."},
	}

	got := selectRecordingSuggestions(items, []string{"капуста"})

	if len(got) != 1 || got[0].Right != "cabbage" {
		t.Fatalf("expected the valid English translation, got %#v", got)
	}
}

func TestRecordingPromptsKeepRussianTextAfterSixThousandRunes(t *testing.T) {
	transcript := strings.Repeat("a", 6001) + " капуста"
	suggestionsPrompt := recordingDetectorPrompt(findAnalysisPass(categoryLanguageSwitch), recordingAnalysisInput{
		Transcript:   transcript,
		Topic:        "Long talk",
		PracticeType: "free_talk",
		EnglishLevel: "b1",
		Russian:      extractRussianPhrases(transcript),
	})
	naturalPrompt := recordingNaturalVersionPrompt(
		transcript,
		[]suggestion{{Wrong: "капуста", Right: "cabbage", Explanation: "Use English."}},
		"b1",
	)

	if !strings.Contains(suggestionsPrompt, "капуста") {
		t.Fatal("expected the full transcript in the suggestions prompt")
	}
	if !strings.Contains(naturalPrompt, "капуста") {
		t.Fatal("expected the full transcript in the natural-version prompt")
	}
}

func TestRecordingNaturalVersionPromptUsesLearnerLevelAndExistingCorrections(t *testing.T) {
	prompt := recordingNaturalVersionPrompt(
		"Yesterday I go to the store.",
		[]suggestion{{Wrong: "I go", Right: "I went", Explanation: "Use past tense."}},
		"b1",
	)

	for _, fragment := range []string{
		"Learner level: B1.",
		"Use clear intermediate vocabulary",
		"preserve the speaker's meaning",
		`"wrong":"I go"`,
		`"correctedTranscript"`,
	} {
		if !strings.Contains(prompt, fragment) {
			t.Fatalf("expected prompt to contain %q, got %q", fragment, prompt)
		}
	}
}

func TestRecordingSuggestionsPromptTreatsRussianInsertionsAsCorrections(t *testing.T) {
	prompt := recordingSuggestionsPrompt(
		"I bought капуста for dinner.",
		"My day",
		nil,
		"free_talk",
		nil,
		"b1",
	)

	for _, fragment := range []string{
		"Russian",
		"every Russian word or phrase",
		`exact Russian text in "wrong"`,
		`English translation in "right"`,
		"before ordinary grammar",
		"Find every clear",
	} {
		if !strings.Contains(prompt, fragment) {
			t.Fatalf("expected mixed-language prompt to contain %q, got %q", fragment, prompt)
		}
	}
}

func TestRecordingNaturalVersionPromptRequiresEnglishTranslationForRussianText(t *testing.T) {
	prompt := recordingNaturalVersionPrompt(
		"I bought капуста for dinner.",
		[]suggestion{{Wrong: "капуста", Right: "cabbage", Explanation: "Use the English word."}},
		"b1",
	)

	for _, fragment := range []string{
		"Replace every Russian word or phrase",
		"English-only",
		`"wrong":"капуста"`,
		`"right":"cabbage"`,
	} {
		if !strings.Contains(prompt, fragment) {
			t.Fatalf("expected natural-version prompt to contain %q, got %q", fragment, prompt)
		}
	}
}

func TestNormalizeRecordingProcessingStageAcceptsKnownStages(t *testing.T) {
	for _, stage := range []string{"transcribing", "suggestions", "rewriting"} {
		value := stage
		got := normalizeRecordingProcessingStage(&value)
		if got == nil || *got != stage {
			t.Fatalf("expected %q stage, got %#v", stage, got)
		}
	}
}

func TestNormalizeRecordingProcessingStageRejectsUnknownStage(t *testing.T) {
	value := "unexpected"

	if got := normalizeRecordingProcessingStage(&value); got != nil {
		t.Fatalf("expected unknown stage to be rejected, got %#v", got)
	}
}

func TestNormalizeShadowingStatus(t *testing.T) {
	for _, value := range []string{"pending", "processing", "ready", "failed"} {
		if got := normalizeShadowingStatus(value); got != value {
			t.Fatalf("expected %q, got %q", value, got)
		}
	}
	if got := normalizeShadowingStatus("unexpected"); got != "pending" {
		t.Fatalf("expected unknown shadowing status to become pending, got %q", got)
	}
}

func TestRecordingQuotaAfterSaveUpdatesFreeUsage(t *testing.T) {
	limit := 600
	remaining := 480
	before := quota.RecordingQuota{
		WeeklyLimitSeconds:     &limit,
		WeeklyUsedSeconds:      120,
		WeeklyRemainingSeconds: &remaining,
		MaxSessionSeconds:      600,
	}

	after := recordingQuotaAfterSave(before, 45)

	if after.WeeklyUsedSeconds != 165 || after.WeeklyRemainingSeconds == nil || *after.WeeklyRemainingSeconds != 435 {
		t.Fatalf("unexpected quota after save: %#v", after)
	}
	if *before.WeeklyRemainingSeconds != 480 {
		t.Fatalf("input quota was mutated: %#v", before)
	}
}
