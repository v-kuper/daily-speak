import type { Strength } from "../lib/data";
import { suggestionCategoryLabel } from "../lib/suggestionPresentation";

type StrengthCardProps = {
  strength: Strength;
  id?: string;
  active?: boolean;
  onShowInTranscript?: () => void;
};

export default function StrengthCard({ strength, id, active = false, onShowInTranscript }: StrengthCardProps) {
  return (
    <article id={id} tabIndex={-1} className={`strength-item${active ? " feedback-card-active" : ""}`}>
      <div className="suggestion-badges">
        <span className="suggestion-badge strength-badge">Strength</span>
        <span className="suggestion-badge suggestion-category-badge">
          {suggestionCategoryLabel(strength.category)}
        </span>
      </div>
      <blockquote className="strength-excerpt">“{strength.excerpt}”</blockquote>
      <div className="strength-explanation">{strength.explanation}</div>
      {strength.learningReference && (
        <div className="suggestion-learning">
          <div className="suggestion-learning-title">Rule used well: {strength.learningReference.title}</div>
          <div className="suggestion-learning-summary">{strength.learningReference.summary}</div>
          {strength.learningReference.url && (
            <a href={strength.learningReference.url} target="_blank" rel="noopener noreferrer">Read the rule</a>
          )}
        </div>
      )}
      {onShowInTranscript && (
        <button className="feedback-locate" type="button" onClick={onShowInTranscript}>
          Show in transcript
        </button>
      )}
    </article>
  );
}
