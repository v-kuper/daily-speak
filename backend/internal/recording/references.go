package recording

import (
	"net/url"
	"strings"
)

type learningReferenceDefinition struct {
	Reference  LearningReference
	Categories map[SuggestionCategory]struct{}
}

var learningReferenceCatalog = map[string]learningReferenceDefinition{
	"english-fillers":                newReference("English hesitation phrases", "Use an English filler while finding the next words.", "", CategoryLanguageSwitch, CategoryNaturalness),
	"expressing-ideas-in-english":    newReference("Expressing ideas in English", "Use English words or paraphrases to express your intended meaning.", "", CategoryLanguageSwitch, CategoryVocabulary),
	"subject-verb-agreement":         newReference("Subject-verb agreement", "Match the verb form to the subject in person and number.", "https://dictionary.cambridge.org/us/grammar/british-grammar/subject-verb-agreement", CategoryVerbGrammar),
	"verb-forms":                     newReference("Verb forms", "Choose the verb form required by the tense and construction.", "https://dictionary.cambridge.org/us/grammar/british-grammar/verbs-basic-forms", CategoryVerbGrammar),
	"present-simple-vs-continuous":   newReference("Present simple and continuous", "Choose between habits or states and actions in progress.", "", CategoryVerbGrammar),
	"past-simple-vs-present-perfect": newReference("Past simple and present perfect", "Choose a finished past time or a past event connected to now.", "", CategoryVerbGrammar),
	"modal-verbs":                    newReference("Modal verbs", "Use the base verb after modals and choose the modal that expresses the intended meaning.", "", CategoryVerbGrammar),
	"infinitive-vs-gerund":           newReference("Infinitives and gerunds", "Learn which verbs and constructions take to-infinitives or -ing forms.", "", CategoryVerbGrammar),
	"conditionals":                   newReference("Conditionals", "Match conditional verb forms to real, possible, hypothetical, or past situations.", "", CategoryVerbGrammar, CategorySentenceStructure),
	"articles-a-an-the":              newReference("Articles: a, an, the", "Choose an indefinite or definite article based on how the noun is introduced and identified.", "https://learnenglish.britishcouncil.org/free-resources/grammar/a1-a2-grammar/articles-a-an-the", CategoryNounsDeterminers),
	"zero-article":                   newReference("Zero article", "Recognize contexts where English uses a noun without an article.", "", CategoryNounsDeterminers),
	"countable-vs-uncountable":       newReference("Countable and uncountable nouns", "Use noun forms, articles, and quantities that match countability.", "", CategoryNounsDeterminers),
	"singular-and-plural-nouns":      newReference("Singular and plural nouns", "Match noun number to meaning and surrounding determiners.", "", CategoryNounsDeterminers),
	"quantifiers":                    newReference("Quantifiers", "Choose quantity words that fit countable or uncountable nouns.", "", CategoryNounsDeterminers),
	"prepositions-of-time-and-place": newReference("Prepositions of time and place", "Choose prepositions such as at, on, and in for time and location.", "", CategoryPrepositions),
	"dependent-prepositions":         newReference("Dependent prepositions", "Learn the prepositions selected by particular verbs, adjectives, and nouns.", "https://learnenglish.britishcouncil.org/free-resources/grammar/b1-b2/verbs-prepositions", CategoryPrepositions),
	"word-formation":                 newReference("Word formation", "Choose the noun, verb, adjective, or adverb form required by the sentence.", "", CategoryVocabulary),
	"collocations":                   newReference("Collocations", "Learn words that conventionally occur together in natural English.", "", CategoryVocabulary, CategoryNaturalness),
	"false-friends":                  newReference("False friends", "Distinguish similar-looking words across languages that have different meanings.", "", CategoryVocabulary),
	"word-order":                     newReference("Word order", "Place subjects, verbs, objects, and adverbials in normal English order.", "", CategorySentenceStructure),
	"questions-and-negatives":        newReference("Questions and negatives", "Use auxiliaries and inversion correctly in questions and negative clauses.", "", CategorySentenceStructure, CategoryVerbGrammar),
	"sentence-fragments-and-run-ons": newReference("Sentence boundaries", "Build complete clauses and connect independent ideas clearly.", "", CategorySentenceStructure),
	"relative-clauses":               newReference("Relative clauses", "Use relative words and clause structure to identify or describe a noun.", "", CategorySentenceStructure),
	"pronoun-reference":              newReference("Pronoun reference", "Make each pronoun agree with and clearly refer to its noun.", "", CategorySentenceStructure, CategoryNounsDeterminers),
}

func newReference(title, summary, rawURL string, categories ...SuggestionCategory) learningReferenceDefinition {
	if rawURL != "" {
		parsed, err := url.Parse(rawURL)
		allowedHost := parsed != nil && (parsed.Hostname() == "dictionary.cambridge.org" || parsed.Hostname() == "learnenglish.britishcouncil.org")
		if err != nil || parsed.Scheme != "https" || !allowedHost {
			panic("invalid learning reference URL: " + rawURL)
		}
	}
	categorySet := make(map[SuggestionCategory]struct{}, len(categories))
	for _, category := range categories {
		categorySet[category] = struct{}{}
	}
	return learningReferenceDefinition{
		Reference:  LearningReference{Title: title, Summary: summary, URL: rawURL},
		Categories: categorySet,
	}
}

func ReferenceFor(ruleID string, category SuggestionCategory) *LearningReference {
	cleanedID := strings.TrimSpace(ruleID)
	definition, exists := learningReferenceCatalog[cleanedID]
	if !exists {
		return nil
	}
	if _, compatible := definition.Categories[category]; !compatible {
		return nil
	}
	reference := definition.Reference
	reference.ID = cleanedID
	return &reference
}
