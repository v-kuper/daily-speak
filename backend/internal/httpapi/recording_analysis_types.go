package httpapi

type recordingAnalysisInput struct {
	Transcript   string   `json:"transcript"`
	Topic        string   `json:"topic"`
	Interests    []string `json:"interests,omitempty"`
	PracticeType string   `json:"practiceType"`
	PhotoObject  *string  `json:"photoObject,omitempty"`
	EnglishLevel string   `json:"englishLevel"`
	Russian      []string `json:"requiredRussianPhrases,omitempty"`
}

type analysisPass struct {
	Name         string
	Category     suggestionCategory
	Finds        string
	MustIgnore   string
	AllowedRules []string
}

type analysisCandidate struct {
	ID          string             `json:"id"`
	Wrong       string             `json:"wrong"`
	Right       string             `json:"right"`
	Explanation string             `json:"explanation"`
	Category    suggestionCategory `json:"category"`
	RuleID      string             `json:"ruleId,omitempty"`
	PassIndex   int                `json:"-"`
}

var recordingAnalysisPasses = []analysisPass{
	{
		Name:       "Russian/non-English insertion",
		Category:   categoryLanguageSwitch,
		Finds:      "Find every required Cyrillic word or contiguous phrase and give its natural English equivalent.",
		MustIgnore: "Ignore English grammar, vocabulary, and style. Return exactly one candidate for every required Russian phrase and no other candidates.",
	},
	{
		Name:       "Verb grammar",
		Category:   categoryVerbGrammar,
		Finds:      "Find only tense, aspect, auxiliary, subject-verb agreement, infinitive or gerund, modal, conditional, and verb-form errors.",
		MustIgnore: "Ignore articles, noun countability, prepositions, optional tense restyling, vocabulary preferences, and all Cyrillic text.",
		AllowedRules: []string{
			"subject-verb-agreement", "verb-forms", "present-simple-vs-continuous", "past-simple-vs-present-perfect",
			"modal-verbs", "infinitive-vs-gerund", "conditionals", "questions-and-negatives",
		},
	},
	{
		Name:       "Nouns and determiners",
		Category:   categoryNounsDeterminers,
		Finds:      "Find only article, determiner, singular or plural, countability, and quantifier errors.",
		MustIgnore: "Ignore optional article choices that are both grammatical, verb errors, prepositions, vocabulary preferences, and all Cyrillic text.",
		AllowedRules: []string{
			"articles-a-an-the", "zero-article", "countable-vs-uncountable", "singular-and-plural-nouns", "quantifiers", "pronoun-reference",
		},
	},
	{
		Name:         "Prepositions",
		Category:     categoryPrepositions,
		Finds:        "Find only incorrect, missing, or extra prepositions and dependent-preposition errors.",
		MustIgnore:   "Ignore alternative prepositions that preserve a valid intended meaning, other grammar, style, and all Cyrillic text.",
		AllowedRules: []string{"prepositions-of-time-and-place", "dependent-prepositions"},
	},
	{
		Name:         "Vocabulary",
		Category:     categoryVocabulary,
		Finds:        "Find only wrong words, wrong word forms, false friends, and broken collocations.",
		MustIgnore:   "Ignore more advanced or elegant synonyms when the learner's word is correct, other grammar, style, and all Cyrillic text.",
		AllowedRules: []string{"word-formation", "collocations", "false-friends"},
	},
	{
		Name:       "Sentence structure",
		Category:   categorySentenceStructure,
		Finds:      "Find only word-order, malformed question or negation, fragment, run-on, relative-clause, and broken clause-structure errors.",
		MustIgnore: "Ignore spoken fragments that are natural and understandable in conversation, local grammar owned by another pass, and all Cyrillic text.",
		AllowedRules: []string{
			"conditionals", "word-order", "questions-and-negatives", "sentence-fragments-and-run-ons", "relative-clauses", "pronoun-reference",
		},
	},
	{
		Name:         "Naturalness",
		Category:     categoryNaturalness,
		Finds:        "Find only clearly non-idiomatic wording that native speakers normally would not use.",
		MustIgnore:   "If wording is acceptable conversational English, return no candidate. Ignore accent, fillers, register preferences, stylistic alternative wording, and all Cyrillic text.",
		AllowedRules: []string{"collocations"},
	},
}

func analysisPassCategories() []suggestionCategory {
	out := make([]suggestionCategory, 0, len(recordingAnalysisPasses))
	for _, pass := range recordingAnalysisPasses {
		out = append(out, pass.Category)
	}
	return out
}

func findAnalysisPass(category suggestionCategory) analysisPass {
	for _, pass := range recordingAnalysisPasses {
		if pass.Category == category {
			return pass
		}
	}
	return analysisPass{}
}
