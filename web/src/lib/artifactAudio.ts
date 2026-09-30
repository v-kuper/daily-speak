import { apiFetch, readApiJSON } from "./apiClient";
import { parseMediaPlaybackTicket, type MediaDownloadPayload, type MediaPlaybackTicket } from "./mediaDownload";

type AudioPayload = MediaDownloadPayload & { status?: string; error?: unknown };
type Request = (path: string, init: RequestInit) => Promise<Response>;
const wait = (signal?: AbortSignal): Promise<void> => new Promise((resolve, reject) => {
 const abort = () => { clearTimeout(timer); reject(new DOMException("Aborted", "AbortError")); };
 const timer = setTimeout(() => { signal?.removeEventListener("abort", abort); resolve(); }, 1000);
 if (signal?.aborted) abort(); else signal?.addEventListener("abort", abort, { once: true });
});
// Each listen checks the server. A ready/processing resource never triggers POST,
// including after a download failure or an expired signed URL.
export const requestArtifactAudio = async (path: string, {
 signal, retryFailed = false, request = apiFetch, delay = wait,
}: { signal?: AbortSignal; retryFailed?: boolean; request?: Request; delay?: (signal?: AbortSignal) => Promise<void> } = {}): Promise<MediaPlaybackTicket> => {
 let started = false;
 for (let poll = 0; poll < 120; poll++) {
  const response = await request(path, { method: "GET", cache: "no-store", signal });
  if (!response.ok) throw new Error("Не удалось загрузить аудио. Попробуйте ещё раз.");
  const payload = await readApiJSON<AudioPayload>(response);
  if (!payload) throw new Error("Некорректный ответ аудиосервиса.");
  if (payload.status === "ready" && typeof payload.asset?.id === "string") return parseMediaPlaybackTicket(payload, payload.asset.id);
  if ((payload.status === "not_requested" || (payload.status === "failed" && retryFailed && poll === 0)) && !started) {
   started = true;
   const start = await request(path, { method: "POST", signal });
   if (!start.ok) throw new Error("Не удалось запустить озвучку. Попробуйте ещё раз.");
  } else if (payload.status === "failed") {
   throw new Error(typeof payload.error === "string" ? payload.error : "Не удалось подготовить аудио.");
  } else if (payload.status !== "processing") throw new Error("Аудио пока недоступно.");
  await delay(signal);
 }
 throw new Error("Аудио ещё готовится. Попробуйте прослушать чуть позже.");
};
export const fetchArtifactAudioBytes = async (path: string): Promise<ArrayBuffer> => {
 const ticket = await requestArtifactAudio(path, { retryFailed: true });
 const response = await fetch(ticket.url, { credentials: "omit", cache: "no-store" });
 if (!response.ok || !response.headers.get("Content-Type")?.startsWith("audio/")) throw new Error("Аудио недоступно.");
 const bytes = await response.arrayBuffer();
 if (!bytes.byteLength || bytes.byteLength > 5 * 1024 * 1024) throw new Error("Некорректное аудио.");
 return bytes;
};
