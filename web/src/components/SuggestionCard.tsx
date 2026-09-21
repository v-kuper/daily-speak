import type { Suggestion } from "../lib/data";
import {
  suggestionCategoryLabel,
  suggestionSeverityClass,
  suggestionSeverityLabel
} from "../lib/suggestionPresentation";

type SuggestionCardProps = {
  suggestion: Suggestion;
};

export default function SuggestionCard({ suggestion }: SuggestionCardProps) {
  const categoryLabel = suggestionCategoryLabel(suggestion.category);
  const severityLabel = suggestionSeverityLabel(suggestion.severity);
  const severityClass = suggestionSeverityClass(suggestion.severity);

  return (
    <article className={`suggestion-item${severityClass ? ` ${severityClass}` : ""}`}>
      {(categoryLabel || severityLabel) && (
        <div className="suggestion-badges">
          {categoryLabel && <span className="suggestion-badge suggestion-category-badge">{categoryLabel}</span>}
          {severityLabel && <span className="suggestion-badge suggestion-severity-badge">{severityLabel}</span>}
        </div>
      )}

      <div className="suggestion-wrong">
        <span className="suggestion-wrong-icon" aria-hidden="true">✕</span>
        <span><strong>You said:</strong> {suggestion.wrong}</span>
      </div>
      <div className="suggestion-right">
        <span className="suggestion-right-icon" aria-hidden="true">✓</span>
        <span><strong>Try:</strong> {suggestion.right}</span>
      </div>
      <div className="suggestion-explanation">{suggestion.explanation}</div>

      {suggestion.learningReference && (
        <div className="suggestion-learning">
          <div className="suggestion-learning-title">
            What to study: {suggestion.learningReference.title}
          </div>
          <div className="suggestion-learning-summary">{suggestion.learningReference.summary}</div>
          {suggestion.learningReference.url && (
            <a href={suggestion.learningReference.url} target="_blank" rel="noopener noreferrer">
              Read the rule
            </a>
          )}
        </div>
      )}
    </article>
  );
}
