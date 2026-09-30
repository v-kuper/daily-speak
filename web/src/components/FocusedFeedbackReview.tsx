"use client";
import { useEffect, useId, useRef, useState } from "react";
import type { FeedbackFocus, FocusedFeedback } from "../lib/data";
import type { SavedInterviewTurn } from "../lib/interviewTimeline";
import { focusedSegments } from "../lib/focusedFeedback";
import ArtifactAudioButton from "./ArtifactAudioButton";
import LocalSpeechRecorder from "./LocalSpeechRecorder";

const labels = { praise: "Хороший пример", blocker: "Ошибка", native_tip: "Как сказать естественнее" };
export function FeedbackLegend({ explain = false }: { explain?: boolean }) {
 return <div className="focus-legend" aria-label="Значения цветов в разборе">
  <span><i className="focus-legend-dot focus-praise" aria-hidden="true" /><strong>Хороший пример</strong>{explain && <span>— удачная формулировка</span>}</span>
  <span><i className="focus-legend-dot focus-blocker" aria-hidden="true" /><strong>Ошибка</strong>{explain && <span>— исправление по правилу</span>}</span>
  <span><i className="focus-legend-dot focus-native_tip" aria-hidden="true" /><strong>Естественность</strong>{explain && <span>— как скажет носитель</span>}</span>
 </div>;
}
export default function FocusedFeedbackReview({ feedback, transcript, turns = [], audioBase, showLegend = true, showQuestion = true }: {
 feedback: FocusedFeedback; transcript: string; turns?: SavedInterviewTurn[]; audioBase: string; showLegend?: boolean; showQuestion?: boolean;
}) {
 const titleID = useId();
 const [selected, setSelected] = useState<FeedbackFocus | null>(null);
 const dialog = useRef<HTMLDialogElement | null>(null);
 const previousFocus = useRef<HTMLElement | null>(null);
 useEffect(() => {
  const modal = dialog.current;
  if (!selected || !modal) return;
  previousFocus.current = document.activeElement as HTMLElement | null;
  modal.showModal();
  return () => { modal.close(); previousFocus.current?.focus({ preventScroll: true }); };
 }, [selected]);
 const renderAnswer = (text: string, items: FeedbackFocus[]) => <>
  <div className="transcript-text">{focusedSegments(text, items).map((segment, index) => segment.focus
   ? <button type="button" className={`focus-mark focus-${segment.focus.kind}`} key={segment.focus.id}
      aria-haspopup="dialog" aria-label={`${labels[segment.focus.kind]}: ${segment.text}`} onClick={() => setSelected(segment.focus!)}>{segment.text}</button>
   : <span key={index}>{segment.text}</span>)}</div>
  <div className="focus-summaries">{items.map(item => <button type="button" className={`focus-summary focus-${item.kind}`} key={item.id}
    aria-haspopup="dialog" aria-label={`${item.title}. ${item.kind === "praise" ? `Удачный фрагмент: ${item.originalFragment}` : `Было: ${item.originalFragment}. Стало: ${item.correctedFragment}`}`}
    onClick={() => setSelected(item)}>
    <span className="focus-summary-proof">
      <span className={item.correctedFragment ? "focus-summary-original" : undefined}>{item.originalFragment}</span>
      {item.correctedFragment && <><span aria-hidden="true">→</span><strong>{item.correctedFragment}</strong></>}
    </span>
    <span className="focus-summary-title">{item.kind === "praise" ? item.explanation : item.title}</span>
   </button>)}</div>
 </>;
 return <div className="focused-review">
  {showLegend && <FeedbackLegend />}
  {turns.length ? turns.map(turn => <section key={turn.sequence} className="focus-turn">{showQuestion && <h4>Вопрос {turn.sequence}: {turn.question}</h4>}
   {renderAnswer(turn.answerText, feedback.answers.find(answer => answer.turnSequence === turn.sequence)?.items ?? [])}</section>)
   : renderAnswer(transcript, feedback.answers.find(answer => !answer.turnSequence)?.items ?? [])}
  <dialog ref={dialog} className="focus-dialog"
   onCancel={event => { event.preventDefault(); event.stopPropagation(); setSelected(null); }}
   onClose={event => { event.stopPropagation(); if (!dialog.current?.open) setSelected(null); }}
   aria-labelledby={titleID} onClick={event => { if (event.target === event.currentTarget) { const bounds = event.currentTarget.getBoundingClientRect(); if (event.clientX < bounds.left || event.clientX > bounds.right || event.clientY < bounds.top || event.clientY > bounds.bottom) setSelected(null); } }}>
   {selected && <div className={`focus-card focus-card-${selected.kind}`}>
    <button type="button" className="focus-close" autoFocus onClick={() => setSelected(null)} aria-label="Закрыть карточку">×</button>
    <h3 id={titleID}>{selected.title}</h3>
    <div className="focus-proof"><span>{selected.originalFragment}</span>{selected.correctedFragment && <><span aria-label="заменить на">→</span><strong>{selected.correctedFragment}</strong></>}</div>
    <p>{selected.explanation}</p>
    {selected.microLesson && <details><summary className="focus-topic">{selected.microLesson.title}</summary><ol>{selected.microLesson.points.map(point => <li key={point}>{point}</li>)}</ol></details>}
    {selected.kind !== "praise" && selected.practiceText && <div className="focus-path">
     <div className="focus-example-heading">
      <h4>Пример применения</h4>
      <p>Отдельный пример этого правила — для прослушивания и повторения.</p>
     </div>
     <blockquote aria-label="Пример применения правила">{selected.practiceText}</blockquote>
     <div className="focus-practice-actions">
      <ArtifactAudioButton key={selected.id} path={`${audioBase}/${encodeURIComponent(selected.id)}/audio`} ariaLabel="Прослушать пример" />
      <LocalSpeechRecorder key={`repeat-${selected.id}`} repeatLabel="Повторить пример голосом" />
     </div>
    </div>}
   </div>}
  </dialog>
 </div>;
}
