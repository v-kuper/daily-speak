package httpapi

import (
	"net/url"
	"strings"
)

type learningReferenceDefinition struct {
	Reference  learningReference
	Categories map[suggestionCategory]struct{}
}

var learningReferenceCatalog = map[string]learningReferenceDefinition{
	"subject-verb-agreement":         newReference("Subject-verb agreement", "Match the verb form to the subject in person and number.", "https://dictionary.cambridge.org/us/grammar/british-grammar/subject-verb-agreement", categoryVerbGrammar),
	"verb-forms":                     newReference("Verb forms", "Choose the verb form required by the tense and construction.", "https://dictionary.cambridge.org/us/grammar/british-grammar/verbs-basic-forms", categoryVerbGrammar),
	"present-simple-vs-continuous":   newReference("Present simple and continuous", "Choose between habits or states and actions in progress.", "", categoryVerbGrammar),
	"past-simple-vs-present-perfect": newReference("Past simple and present perfect", "Choose a finished past time or a past event connected to now.", "", categoryVerbGrammar),
	"modal-verbs":                    newReference("Modal verbs", "Use the base verb after modals and choose the modal that expresses the intended meaning.", "", categoryVerbGrammar),
	"infinitive-vs-gerund":           newReference("Infinitives and gerunds", "Learn which verbs and constructions take to-infinitives or -ing forms.", "", categoryVerbGrammar),
	"conditionals":                   newReference("Conditionals", "Match conditional verb forms to real, possible, hypothetical, or past situations.", "", categoryVerbGrammar, categorySentenceStructure),
	"articles-a-an-the":              newReference("Articles: a, an, the", "Choose an indefinite or definite article based on how the noun is introduced and identified.", "https://learnenglish.britishcouncil.org/free-resources/grammar/a1-a2-grammar/articles-a-an-the", categoryNounsDeterminers),
	"zero-article":                   newReference("Zero article", "Recognize contexts where English uses a noun without an article.", "", categoryNounsDeterminers),
	"countable-vs-uncountable":       newReference("Countable and uncountable nouns", "Use noun forms, articles, and quantities that match countability.", "", categoryNounsDeterminers),
	"singular-and-plural-nouns":      newReference("Singular and plural nouns", "Match noun number to meaning and surrounding determiners.", "", categoryNounsDeterminers),
	"quantifiers":                    newReference("Quantifiers", "Choose quantity words that fit countable or uncountable nouns.", "", categoryNounsDeterminers),
	"prepositions-of-time-and-place": newReference("Prepositions of time and place", "Choose prepositions such as at, on, and in for time and location.", "", categoryPrepositions),
	"dependent-prepositions":         newReference("Dependent prepositions", "Learn the prepositions selected by particular verbs, adjectives, and nouns.", "https://learnenglish.britishcouncil.org/free-resources/grammar/b1-b2/verbs-prepositions", categoryPrepositions),
	"word-formation":                 newReference("Word formation", "Choose the noun, verb, adjective, or adverb form required by the sentence.", "", categoryVocabulary),
	"collocations":                   newReference("Collocations", "Learn words that conventionally occur together in natural English.", "", categoryVocabulary, categoryNaturalness),
	"false-friends":                  newReference("False friends", "Distinguish similar-looking words across languages that have different meanings.", "", categoryVocabulary),
	"word-order":                     newReference("Word order", "Place subjects, verbs, objects, and adverbials in normal English order.", "", categorySentenceStructure),
	"questions-and-negatives":        newReference("Questions and negatives", "Use auxiliaries and inversion correctly in questions and negative clauses.", "", categorySentenceStructure, categoryVerbGrammar),
	"sentence-fragments-and-run-ons": newReference("Sentence boundaries", "Build complete clauses and connect independent ideas clearly.", "", categorySentenceStructure),
	"relative-clauses":               newReference("Relative clauses", "Use relative words and clause structure to identify or describe a noun.", "", categorySentenceStructure),
	"pronoun-reference":              newReference("Pronoun reference", "Make each pronoun agree with and clearly refer to its noun.", "", categorySentenceStructure, categoryNounsDeterminers),
}

func newReference(title, summary, rawURL string, categories ...suggestionCategory) learningReferenceDefinition {
	if rawURL != "" {
		parsed, err := url.Parse(rawURL)
		allowedHost := parsed != nil && (parsed.Hostname() == "dictionary.cambridge.org" || parsed.Hostname() == "learnenglish.britishcouncil.org")
		if err != nil || parsed.Scheme != "https" || !allowedHost {
			panic("invalid learning reference URL: " + rawURL)
		}
	}
	categorySet := make(map[suggestionCategory]struct{}, len(categories))
	for _, category := range categories {
		categorySet[category] = struct{}{}
	}
	return learningReferenceDefinition{
		Reference:  learningReference{Title: title, Summary: summary, URL: rawURL},
		Categories: categorySet,
	}
}

func learningReferenceFor(ruleID string, category suggestionCategory) *learningReference {
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
