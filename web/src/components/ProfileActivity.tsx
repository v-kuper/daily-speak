"use client";

import { useCallback, useEffect, useId, useRef, useState } from "react";
import { apiFetch, readApiJSON } from "../lib/apiClient";
import { activityMinutesLabel, activityTimeLabel, heatmapWeeks, parseActivitySummary, type ActivityDay, type ActivitySummary } from "../lib/activity";
import { flushActivity } from "../lib/activityTracking";

const dateLabel = (key: string) => new Date(`${key}T12:00:00Z`).toLocaleDateString("ru-RU", { day: "numeric", month: "long", year: "numeric", timeZone: "UTC" });
const detailLabel = (day: ActivityDay) => `${dateLabel(day.date)}. Разговорная практика: ${activityMinutesLabel(day.speakingMilliseconds)}. Разбор ошибок: ${activityMinutesLabel(day.reviewMilliseconds)}.`;

function ActivityCalendar({ summary, mobile = false }: { summary: ActivitySummary; mobile?: boolean }) {
  const weeks = heatmapWeeks(summary.days, mobile);
  const tooltipID = useId();
  const root = useRef<HTMLDivElement>(null);
  const [selected, setSelected] = useState<{ day: ActivityDay; left: number; top: number } | null>(null);
  useEffect(() => {
    const close = (event: PointerEvent) => { if (!root.current?.contains(event.target as Node)) setSelected(null); };
    const escape = (event: KeyboardEvent) => { if (event.key === "Escape") setSelected(null); };
    document.addEventListener("pointerdown", close); document.addEventListener("keydown", escape);
    return () => { document.removeEventListener("pointerdown", close); document.removeEventListener("keydown", escape); };
  }, []);
  const show = (day: ActivityDay, button: HTMLElement) => {
    const container = root.current?.getBoundingClientRect();
    if (!container) return;
    const rect = button.getBoundingClientRect();
    setSelected({ day, left: Math.max(110, Math.min(container.width - 110, rect.left - container.left + rect.width / 2)), top: rect.bottom - container.top + 8 });
  };
  let previousMonth = "";
  return <div ref={root} className={`activity-calendar activity-calendar-${mobile ? "mobile" : "desktop"}`}
    onBlur={event => { if (!event.currentTarget.contains(event.relatedTarget)) setSelected(null); }}>
    <div className="activity-months" aria-hidden="true" style={{ gridTemplateColumns: `repeat(${weeks.length}, minmax(0, 1fr))` }}>
      {weeks.map((week, index) => {
        const day = week.find(Boolean);
        const month = day?.date.slice(0, 7) ?? "";
        const changed = month !== previousMonth;
        previousMonth = month;
        return <span key={index} style={{ gridColumn: index + 1 }}>{changed && day ? new Date(`${day.date}T12:00:00Z`).toLocaleDateString("ru-RU", { month: "short", timeZone: "UTC" }).replace(".", "") : ""}</span>;
      })}
    </div>
    <div className="activity-grid-wrap">
      <div className="activity-weekdays" aria-hidden="true"><span>Пн</span><span /><span>Ср</span><span /><span>Пт</span><span /><span /></div>
      <div className="activity-grid" style={{ gridTemplateColumns: `repeat(${weeks.length}, minmax(0, 1fr))` }} aria-label={mobile ? "Активность за 24 недели" : "Активность за последние 12 месяцев"}>
        {weeks.flatMap((week, column) => week.map((day, row) => day ? <button type="button" key={day.date}
          className={`activity-day activity-level-${day.level}`}
          style={{ gridColumn: column + 1, gridRow: row + 1 }}
          aria-label={detailLabel(day)} aria-describedby={selected?.day.date === day.date ? tooltipID : undefined}
          onClick={event => show(day, event.currentTarget)} onFocus={event => show(day, event.currentTarget)}
          onMouseEnter={event => show(day, event.currentTarget)}
          onMouseLeave={event => { if (document.activeElement !== event.currentTarget) setSelected(null); }}
        /> : <span aria-hidden="true" className="activity-day activity-day-outside" key={`${column}:${row}`} style={{ gridColumn: column + 1, gridRow: row + 1 }} />))}
      </div>
    </div>
    {selected && <div id={tooltipID} role="tooltip" className="activity-tooltip" style={{ left: selected.left, top: selected.top }}>
      <strong>{dateLabel(selected.day.date)}</strong>
      <span>⏱ Разговорная практика: {activityMinutesLabel(selected.day.speakingMilliseconds)}</span>
      <span>📝 Разбор ошибок: {activityMinutesLabel(selected.day.reviewMilliseconds)}</span>
    </div>}
  </div>;
}

export default function ProfileActivity() {
  const [summary, setSummary] = useState<ActivitySummary | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [refresh, setRefresh] = useState(0);
  const generation = useRef(0);
  const load = useCallback(async (signal: AbortSignal) => {
    const request = ++generation.current;
    setLoading(true); setError("");
    try {
      await flushActivity();
      const timezone = Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
      const response = await apiFetch(`/api/v1/profile/activity?timezone=${encodeURIComponent(timezone)}`, { signal });
      if (!response.ok) throw new Error("Не удалось загрузить статистику. Попробуйте ещё раз.");
      const result = parseActivitySummary(await readApiJSON(response));
      if (!signal.aborted && request === generation.current) setSummary(result);
    } catch (failure) {
      if (!signal.aborted && request === generation.current) setError(failure instanceof Error ? failure.message : "Не удалось загрузить статистику.");
    } finally { if (!signal.aborted && request === generation.current) setLoading(false); }
  }, []);
  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal);
    const update = () => { if (!document.hidden) void load(controller.signal); };
    document.addEventListener("visibilitychange", update);
    return () => { controller.abort(); document.removeEventListener("visibilitychange", update); };
  }, [load, refresh]);

  return <div className="profile-activity" aria-busy={loading}>
    <div className="activity-total-card">
      <p className="activity-total-label">Вы в разговоре:</p>
      {summary ? <p className="activity-total-value">{activityTimeLabel(summary.totalSpeakingMilliseconds)}</p>
        : <p className="activity-total-placeholder" role="status">{loading ? "Считаем время практики…" : "Статистика пока недоступна"}</p>}
      <p className="activity-total-caption">Каждая минута практики остаётся с вами.</p>
    </div>
    <section className="activity-heatmap-card" aria-labelledby="activity-calendar-title">
      <div className="activity-heading"><h3 id="activity-calendar-title">Ваша практика</h3><span className="activity-period-desktop">Последние 12 месяцев</span><span className="activity-period-mobile">Последние 24 недели</span></div>
      <p className="activity-calendar-caption">Разговоры и разбор ошибок — день за днём.</p>
      {summary && <>
        <ActivityCalendar summary={summary} />
        <ActivityCalendar summary={summary} mobile />
        <div className="activity-legend" aria-label="Шкала активности: нет активности, до 5 минут включительно, больше 5 и меньше 15 минут, от 15 минут">
          <span>Меньше</span>{[0, 1, 2, 3].map(level => <i key={level} className={`activity-day activity-level-${level}`} aria-hidden="true" />)}<span>Больше</span>
        </div>
        <p className="activity-calendar-hint">Нажмите на день, чтобы посмотреть время практики.</p>
        {summary.historicalSpeakingMilliseconds > 0 && <p className="activity-history-note">Ранние сессии учтены по общей длительности сохранённых записей.</p>}
      </>}
      {!summary && loading && <div className="activity-loading" role="status">Загружаем календарь…</div>}
      {error && <div className="activity-error"><p role="alert">{error}</p><button className="btn btn-secondary btn-small" type="button" disabled={loading} onClick={() => setRefresh(value => value + 1)}>Повторить</button></div>}
    </section>
  </div>;
}
