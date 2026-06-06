package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/ai"
	"daily-speaking-practice/backend/internal/domain"
	"daily-speaking-practice/backend/internal/logging"
)

const (
	dailyQuestionsCount       = 3
	topicGuidanceQuestionsCnt = 10
	topicGuidanceWordsCnt     = 8
)

var dateKeyPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func (s *Server) handleDailyQuestions(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	logger := logging.ForRequest("api.daily-questions.get", r)
	values := r.URL.Query()
	dateKey := values.Get("date")
	refreshToken := values.Get("refresh")
	interests := normalizeURLInterests(values)
	avoidRaw := values["avoid"]
	avoidQuestions := normalizeQuestions(avoidRaw, dailyQuestionsCount)
	avoidLower := lowerSet(normalizeQuestions(avoidRaw, dailyQuestionsCount))

	if dateKey == "" || !dateKeyPattern.MatchString(dateKey) {
		logger.Warn("request.rejected", map[string]any{"status": 400, "durationMs": logging.ElapsedMs(started), "reason": "invalid_date"})
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Query param `date` must be in YYYY-MM-DD format."})
		return
	}

	user, err := s.optionalUser(r)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load session."})
		return
	}
	englishLevel := domain.NormalizeEnglishLevel(values.Get("level"))
	if user != nil {
		englishLevel = user.EnglishLevel
	}
	settings := ai.ResolveSettingsForUser()
	dateSeed, _ := strconvAtoi(strings.ReplaceAll(dateKey, "-", ""))
	interestsSeed := domain.HashString(strings.ToLower(strings.Join(interests, "|")))
	levelSeed := domain.HashString(englishLevel)
	refreshSeed := 0
	hasRefreshSeed := false
	if refreshToken != "" {
		digits := regexp.MustCompile(`\D`).ReplaceAllString(refreshToken, "")
		if parsed, ok := parseIntOK(digits); ok {
			refreshSeed = parsed
			hasRefreshSeed = true
		}
	}
	seed := absMod(dateSeed*131+interestsSeed*17+levelSeed*19+refreshSeed, 2147483647)

	for attempt := 0; attempt < 3; attempt++ {
		attemptNumber := attempt + 1
		attemptSeed := absMod(seed+(attempt+1)*9973, 2147483647)
		body := map[string]any{
			"model":  settings.Model,
			"stream": false,
			"think":  ai.ThinkOption(settings.IsThinkingModel),
			"messages": []map[string]string{
				{"role": "system", "content": "You are an assistant that generates concise speaking-practice questions and always follows output format exactly."},
				{"role": "user", "content": dailyQuestionsPrompt(dateKey, refreshToken, englishLevel, interests, avoidQuestions)},
			},
			"options": map[string]any{
				"temperature": chooseFloat(hasRefreshSeed, 0.7+float64(attempt)*0.08, 0.2+float64(attempt)*0.05),
				"seed":        attemptSeed,
			},
		}
		payload, _, err := ai.PostChat(r.Context(), body)
		if err != nil {
			writeAIError(w, err, "Cannot connect to local Ollama. Check OLLAMA_BASE_URL and running Ollama service.")
			return
		}
		content := ai.ExtractMessageContent(payload)
		if content == "" {
			continue
		}
		questions, ok := parseQuestions(content, dailyQuestionsCount)
		if !ok || anyLowerOverlap(questions, avoidLower) {
			continue
		}
		logger.Info("request.success", map[string]any{"status": 200, "durationMs": logging.ElapsedMs(started), "model": settings.Model, "attempt": attemptNumber})
		writeJSON(w, http.StatusOK, map[string]any{"questions": questions})
		return
	}

	writeJSON(w, http.StatusBadGateway, map[string]string{"error": "Could not generate a sufficiently new set of questions. Try regenerate again."})
}

func (s *Server) handleTopicGuidance(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	logger := logging.ForRequest("api.topic-guidance.get", r)
	values := r.URL.Query()
	topic := strings.TrimSpace(values.Get("topic"))
	refreshToken := values.Get("refresh")
	interests := normalizeURLInterests(values)
	avoidQuestionsRaw := normalizeQuestions(values["avoidQuestion"], 0)
	avoidWordsRaw := normalizeWords(values["avoidWord"])
	avoidQuestions := lowerSet(avoidQuestionsRaw)
	avoidWords := lowerSet(avoidWordsRaw)

	if topic == "" {
		logger.Warn("request.rejected", map[string]any{"status": 400, "durationMs": logging.ElapsedMs(started), "reason": "missing_topic"})
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Query param `topic` is required."})
		return
	}
	if len([]rune(topic)) > 300 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Topic is too long."})
		return
	}

	user, err := s.optionalUser(r)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load session."})
		return
	}
	englishLevel := domain.NormalizeEnglishLevel(values.Get("level"))
	if user != nil {
		englishLevel = user.EnglishLevel
	}
	settings := ai.ResolveSettingsForUser()
	seed := absMod(domain.HashString(strings.ToLower(topic))*131+domain.HashString(strings.ToLower(strings.Join(interests, "|")))*17+domain.HashString(englishLevel)*19+domain.HashString(refreshToken), 2147483647)

	for attempt := 0; attempt < 3; attempt++ {
		attemptNumber := attempt + 1
		body := map[string]any{
			"model":  settings.Model,
			"stream": false,
			"think":  ai.ThinkOption(settings.IsThinkingModel),
			"messages": []map[string]string{
				{"role": "system", "content": "You generate concise speaking-practice guidance and must follow the output format exactly."},
				{"role": "user", "content": topicGuidancePrompt(topic, refreshToken, englishLevel, interests, avoidQuestionsRaw, avoidWordsRaw)},
			},
			"options": map[string]any{
				"temperature": chooseFloat(refreshToken != "", 0.7+float64(attempt)*0.08, 0.2+float64(attempt)*0.05),
				"seed":        absMod(seed+(attempt+1)*7919, 2147483647),
			},
		}
		payload, _, err := ai.PostChat(r.Context(), body)
		if err != nil {
			writeAIError(w, err, "Cannot connect to local Ollama. Check OLLAMA_BASE_URL and running Ollama service.")
			return
		}
		guidance, ok := parseTopicGuidance(ai.ExtractMessageContent(payload))
		if !ok || anyLowerOverlap(guidance.Questions, avoidQuestions) || anyLowerOverlap(guidance.Words, avoidWords) {
			continue
		}
		logger.Info("request.success", map[string]any{"status": 200, "durationMs": logging.ElapsedMs(started), "model": settings.Model, "attempt": attemptNumber})
		writeJSON(w, http.StatusOK, guidance)
		return
	}
	writeJSON(w, http.StatusBadGateway, map[string]string{"error": "Could not generate sufficiently new guidance. Try regenerate again."})
}

func (s *Server) handleStudyWords(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	logger := logging.ForRequest("api.study-words.get", r)
	values := r.URL.Query()
	refreshToken := values.Get("refresh")
	interests := normalizeURLInterests(values)
	avoidWords := normalizeAvoidWords(values["avoidWord"])
	user, err := s.optionalUser(r)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load session."})
		return
	}
	englishLevel := domain.NormalizeEnglishLevel(values.Get("level"))
	if user != nil {
		englishLevel = user.EnglishLevel
	}
	settings := ai.ResolveSettingsForUser()
	seed := absMod(domain.HashString(englishLevel)*131+domain.HashString(strings.ToLower(strings.Join(interests, "|")))*17+domain.HashString(strings.ToLower(strings.Join(avoidWords, "|")))*19+domain.HashString(refreshToken), 2147483647)

	for attempt := 0; attempt < 3; attempt++ {
		attemptNumber := attempt + 1
		body := map[string]any{
			"model":  settings.Model,
			"stream": false,
			"think":  ai.ThinkOption(settings.IsThinkingModel),
			"messages": []map[string]string{
				{"role": "system", "content": "You generate level-appropriate vocabulary packs and must follow the JSON output format exactly."},
				{"role": "user", "content": studyWordsPrompt(englishLevel, interests, refreshToken, avoidWords)},
			},
			"options": map[string]any{
				"temperature": chooseFloat(refreshToken != "", 0.68+float64(attempt)*0.08, 0.22+float64(attempt)*0.05),
				"seed":        absMod(seed+(attempt+1)*9157, 2147483647),
			},
		}
		payload, _, err := ai.PostChat(r.Context(), body)
		if err != nil {
			writeAIError(w, err, "Cannot connect to local Ollama. Check OLLAMA_BASE_URL and running Ollama service.")
			return
		}
		pack, ok := parseStudyPack(ai.ExtractMessageContent(payload), avoidWords)
		if !ok {
			continue
		}
		logger.Info("request.success", map[string]any{"status": 200, "durationMs": logging.ElapsedMs(started), "model": settings.Model, "attempt": attemptNumber, "wordsCount": len(pack.Words)})
		writeJSON(w, http.StatusOK, pack)
		return
	}
	writeJSON(w, http.StatusBadGateway, map[string]string{"error": "Could not generate a valid words pack. Try regenerate."})
}

func dailyQuestionsPrompt(dateKey string, refreshToken string, englishLevel string, interests []string, avoidQuestions []string) string {
	parts := []string{
		fmt.Sprintf("Generate exactly %d daily English speaking practice questions for %s.", dailyQuestionsCount, dateKey),
		"Audience: English learner level " + domain.FormatEnglishLevel(englishLevel) + ".",
		"Language difficulty: " + domain.EnglishLevelPromptGuidance(englishLevel),
		"Questions must be short, practical, and suitable for a 1-3 minute spoken answer.",
		"Each question must be clearly tied to a concrete theme, not a generic life question.",
		"The theme should be explicit in the wording of each question.",
		fmt.Sprintf("All %d questions must be semantically different from each other.", dailyQuestionsCount),
		`Return only JSON with this exact shape: {"questions":["question 1","question 2","question 3"]}.`,
		"Do not add markdown, explanations, numbering, or extra keys.",
	}
	if len(interests) > 0 {
		parts = append(parts, "User interests (use as themes): "+strings.Join(interests, ", ")+".")
		parts = append(parts, "Each question must map to one of these interests and mention that theme explicitly.")
		parts = append(parts, fmt.Sprintf("Use different interests across the %d questions when possible.", dailyQuestionsCount))
		parts = append(parts, "Do not generate off-topic or generic questions unrelated to the listed interests.")
	}
	if refreshToken != "" {
		parts = append(parts, "Variation key: "+refreshToken+". Return a different set than earlier generations for the same date.")
	}
	if len(avoidQuestions) > 0 {
		parts = append(parts, "Do not reuse any of these previous questions: "+strings.Join(avoidQuestions, " | ")+".")
		parts = append(parts, "If a candidate is similar, replace it with a new angle.")
	}
	return strings.Join(parts, " ")
}

func topicGuidancePrompt(topic string, refreshToken string, englishLevel string, interests []string, avoidQuestions []string, avoidWords []string) string {
	parts := []string{
		`Topic: "` + topic + `".`,
		"Generate guidance for an English speaking practice session for level " + domain.FormatEnglishLevel(englishLevel) + ".",
		"Language difficulty: " + domain.EnglishLevelPromptGuidance(englishLevel),
		fmt.Sprintf("Return exactly %d follow-up questions for speaking practice.", topicGuidanceQuestionsCnt),
		fmt.Sprintf("Return exactly %d useful words or short phrases connected to this topic.", topicGuidanceWordsCnt),
		"Useful words must match the learner level and stay understandable for that level.",
		"All follow-up questions should be distinct in angle and not paraphrases of each other.",
		"Useful words should be diverse, not near-duplicates.",
		`Return only JSON with this exact shape: {"questions":["q1","q2","q3","q4","q5","q6","q7","q8","q9","q10"],"words":["w1","w2","w3","w4","w5","w6","w7","w8"]}.`,
		"No markdown, no extra keys, no explanations.",
	}
	if len(interests) > 0 {
		parts = append(parts, "Learner interests: "+strings.Join(interests, ", ")+".")
		parts = append(parts, "Keep questions and useful words relevant to these interests when possible.")
	}
	if refreshToken != "" {
		parts = append(parts, "Variation key: "+refreshToken+". Make a different set than previous outputs.")
	}
	if len(avoidQuestions) > 0 {
		parts = append(parts, "Do not reuse any of these previous follow-up questions: "+strings.Join(avoidQuestions, " | ")+".")
	}
	if len(avoidWords) > 0 {
		parts = append(parts, "Do not reuse any of these previous useful words/phrases: "+strings.Join(avoidWords, " | ")+".")
	}
	return strings.Join(parts, " ")
}

func studyWordsPrompt(englishLevel string, interests []string, refreshToken string, avoidWords []string) string {
	parts := []string{
		"Generate vocabulary for English speaking/reading study.",
		"Learner level: " + domain.FormatEnglishLevel(englishLevel) + ".",
		"Language difficulty: " + domain.EnglishLevelPromptGuidance(englishLevel),
		"Return exactly 10 useful English words (single words or short 2-word terms).",
		"Then write one cohesive text (120-180 words) that naturally uses these words in context.",
		"The text must be clear and practical so learner understands usage context.",
		`Return only JSON with this exact shape: {"words":["w1","w2","w3","w4","w5","w6","w7","w8","w9","w10"],"text":"..."}`,
		"No markdown, no extra keys, no explanations.",
	}
	if len(interests) > 0 {
		parts = append(parts, "Prefer topics connected to these interests: "+strings.Join(interests, ", ")+".")
	}
	if len(avoidWords) > 0 {
		parts = append(parts, "Do not reuse these words: "+strings.Join(avoidWords, ", ")+".")
	}
	if refreshToken != "" {
		parts = append(parts, "Variation key: "+refreshToken+". Generate a different set than previous outputs.")
	}
	return strings.Join(parts, " ")
}

type topicGuidance struct {
	Questions []string `json:"questions"`
	Words     []string `json:"words"`
}

type studyPack struct {
	Words []string `json:"words"`
	Text  string   `json:"text"`
}

func parseQuestions(content string, limit int) ([]string, bool) {
	for _, candidate := range ai.ExtractJSONCandidates(content) {
		var payload struct {
			Questions []string `json:"questions"`
		}
		if json.Unmarshal([]byte(candidate), &payload) == nil {
			questions := normalizeQuestions(payload.Questions, limit)
			if len(questions) == limit {
				return questions, true
			}
		}
	}
	lines := nonEmptyLines(ai.NormalizeContent(content))
	questions := normalizeQuestions(lines, limit)
	return questions, len(questions) == limit
}

func parseTopicGuidance(content string) (topicGuidance, bool) {
	tryParse := func(candidate string) (topicGuidance, bool) {
		var payload struct {
			Questions []string `json:"questions"`
			Words     []string `json:"words"`
		}
		if json.Unmarshal([]byte(candidate), &payload) != nil {
			return topicGuidance{}, false
		}
		questions := normalizeQuestions(payload.Questions, 0)
		words := normalizeWords(payload.Words)
		if len(questions) >= topicGuidanceQuestionsCnt && len(words) >= 5 {
			return topicGuidance{Questions: questions[:topicGuidanceQuestionsCnt], Words: words[:min(len(words), topicGuidanceWordsCnt)]}, true
		}
		return topicGuidance{}, false
	}
	if parsed, ok := tryParse(content); ok {
		return parsed, true
	}
	for _, candidate := range ai.ExtractJSONCandidates(content) {
		if parsed, ok := tryParse(candidate); ok {
			return parsed, true
		}
	}
	lines := nonEmptyLines(ai.NormalizeContent(content))
	questionLines := []string{}
	wordLines := []string{}
	for _, line := range lines {
		if strings.Contains(line, "?") || regexp.MustCompile(`^\d+\s*[\)\.\-:]`).MatchString(line) {
			questionLines = append(questionLines, line)
		} else {
			wordLines = append(wordLines, line)
		}
	}
	questions := normalizeQuestions(questionLines, 0)
	words := normalizeWords(wordLines)
	if len(questions) >= topicGuidanceQuestionsCnt && len(words) >= 5 {
		return topicGuidance{Questions: questions[:topicGuidanceQuestionsCnt], Words: words[:min(len(words), topicGuidanceWordsCnt)]}, true
	}
	return topicGuidance{}, false
}

func parseStudyPack(content string, avoidWords []string) (studyPack, bool) {
	for _, candidate := range ai.ExtractJSONCandidates(content) {
		var payload struct {
			Words      []string `json:"words"`
			Vocabulary []string `json:"vocabulary"`
			Text       string   `json:"text"`
			Story      string   `json:"story"`
			Paragraph  string   `json:"paragraph"`
		}
		if json.Unmarshal([]byte(candidate), &payload) != nil {
			continue
		}
		wordsRaw := payload.Words
		if len(wordsRaw) == 0 {
			wordsRaw = payload.Vocabulary
		}
		text := normalizeText(firstNonEmpty(payload.Text, payload.Story, payload.Paragraph))
		words := normalizeWordList(wordsRaw)
		if validStudyPack(words, text, avoidWords) {
			return studyPack{Words: words[:10], Text: text}, true
		}
	}
	lines := nonEmptyLines(ai.NormalizeContent(content))
	if len(lines) < 4 {
		return studyPack{}, false
	}
	wordsLine := ""
	for _, line := range lines {
		if regexp.MustCompile(`(?i)^words?\s*:`).MatchString(line) || strings.Contains(line, ",") {
			wordsLine = line
			break
		}
	}
	candidateWords := []string{}
	if wordsLine != "" {
		cleaned := regexp.MustCompile(`(?i)^words?\s*:`).ReplaceAllString(wordsLine, "")
		candidateWords = regexp.MustCompile(`[,|]`).Split(cleaned, -1)
	} else {
		candidateWords = lines[:min(len(lines), 10)]
	}
	textLines := []string{}
	for _, line := range lines {
		if line != wordsLine {
			textLines = append(textLines, line)
		}
	}
	words := normalizeWordList(candidateWords)
	text := normalizeText(strings.Join(textLines, "\n"))
	if validStudyPack(words, text, avoidWords) {
		return studyPack{Words: words[:10], Text: text}, true
	}
	return studyPack{}, false
}

func normalizeQuestions(items []string, limit int) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, raw := range items {
		cleaned := strings.TrimSpace(raw)
		cleaned = regexp.MustCompile(`^\d+\s*[\)\.\-:]\s*`).ReplaceAllString(cleaned, "")
		cleaned = regexp.MustCompile(`^[-*]\s*`).ReplaceAllString(cleaned, "")
		cleaned = strings.Join(strings.Fields(cleaned), " ")
		if cleaned == "" {
			continue
		}
		if !strings.HasSuffix(cleaned, "?") {
			cleaned += "?"
		}
		key := strings.ToLower(cleaned)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, cleaned)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func normalizeWords(items []string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, raw := range items {
		cleaned := strings.TrimSpace(raw)
		cleaned = regexp.MustCompile(`^\d+\s*[\)\.\-:]\s*`).ReplaceAllString(cleaned, "")
		cleaned = regexp.MustCompile(`^[-*]\s*`).ReplaceAllString(cleaned, "")
		cleaned = regexp.MustCompile(`[.;]+$`).ReplaceAllString(cleaned, "")
		cleaned = strings.Join(strings.Fields(cleaned), " ")
		if cleaned == "" {
			continue
		}
		key := strings.ToLower(cleaned)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, cleaned)
	}
	return out
}

func normalizeAvoidWords(items []string) []string {
	return uniqueLowerPreserve(normalizeWordList(items))
}

func normalizeWordList(items []string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, raw := range items {
		word := normalizeWord(raw)
		if word == "" {
			continue
		}
		if len(strings.Fields(word)) > 3 || len([]rune(word)) > 36 {
			continue
		}
		key := strings.ToLower(word)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, word)
	}
	return out
}

func normalizeWord(value string) string {
	out := strings.TrimSpace(value)
	out = regexp.MustCompile(`^\d+\s*[\)\.\-:]\s*`).ReplaceAllString(out, "")
	out = regexp.MustCompile(`^[-*]\s*`).ReplaceAllString(out, "")
	out = strings.Trim(out, "\"'`")
	out = regexp.MustCompile(`[.,;:!?]+$`).ReplaceAllString(out, "")
	out = strings.Join(strings.Fields(out), " ")
	return out
}

func normalizeText(value string) string {
	out := strings.TrimSpace(value)
	out = strings.ReplaceAll(out, "\r\n", "\n")
	out = regexp.MustCompile(`\n{3,}`).ReplaceAllString(out, "\n\n")
	out = regexp.MustCompile(`[ \t]+`).ReplaceAllString(out, " ")
	if len([]rune(out)) > 20000 {
		out = string([]rune(out)[:20000])
	}
	return out
}

func validStudyPack(words []string, text string, avoidWords []string) bool {
	if len(words) != 10 || countWords(text) < 80 || anyLowerOverlap(words, lowerSet(avoidWords)) {
		return false
	}
	return countMatchedWords(text, words) >= 7
}

func countWords(text string) int {
	return len(strings.Fields(strings.TrimSpace(text)))
}

func countMatchedWords(text string, words []string) int {
	lowerText := strings.ToLower(text)
	count := 0
	for _, word := range words {
		escaped := regexp.QuoteMeta(strings.ToLower(word))
		if regexp.MustCompile(`\b` + escaped + `\b`).MatchString(lowerText) {
			count++
		}
	}
	return count
}

func nonEmptyLines(value string) []string {
	out := []string{}
	for _, line := range strings.Split(value, "\n") {
		cleaned := strings.TrimSpace(line)
		if cleaned != "" {
			out = append(out, cleaned)
		}
	}
	return out
}

func lowerSet(items []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, item := range items {
		cleaned := strings.ToLower(strings.TrimSpace(item))
		if cleaned != "" {
			out[cleaned] = struct{}{}
		}
	}
	return out
}

func anyLowerOverlap(items []string, avoid map[string]struct{}) bool {
	if len(avoid) == 0 {
		return false
	}
	for _, item := range items {
		if _, ok := avoid[strings.ToLower(item)]; ok {
			return true
		}
	}
	return false
}

func uniqueLowerPreserve(items []string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, item := range items {
		key := strings.ToLower(item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func writeAIError(w http.ResponseWriter, err error, connectionMessage string) {
	var chatErr ai.ChatError
	if errorsAs(err, &chatErr) {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": chatErr.Message})
		return
	}
	writeJSON(w, http.StatusBadGateway, map[string]string{"error": connectionMessage})
}

func chooseFloat(condition bool, ifTrue float64, ifFalse float64) float64 {
	if condition {
		return ifTrue
	}
	return ifFalse
}

func parseIntOK(value string) (int, bool) {
	if value == "" {
		return 0, false
	}
	parsed, err := strconvAtoi(value)
	return parsed, err == nil
}

func strconvAtoi(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("empty")
	}
	sign := 1
	if strings.HasPrefix(value, "-") {
		sign = -1
		value = strings.TrimPrefix(value, "-")
	}
	total := 0
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("invalid int")
		}
		total = total*10 + int(r-'0')
	}
	return total * sign, nil
}

func absMod(value int, mod int) int {
	if mod <= 0 {
		return value
	}
	out := value % mod
	if out < 0 {
		return -out
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func min(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func errorsAs(err error, target any) bool {
	switch typed := target.(type) {
	case *ai.ChatError:
		if value, ok := err.(ai.ChatError); ok {
			*typed = value
			return true
		}
		if value, ok := err.(*ai.ChatError); ok {
			*typed = *value
			return true
		}
	}
	return false
}

func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
