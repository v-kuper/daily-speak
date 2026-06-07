export const AUDIO_MIME_CANDIDATES = ["audio/webm;codecs=opus", "audio/webm", "audio/mp4", "audio/ogg;codecs=opus"];

const AUDIO_EXTENSION_BY_MIME: Record<string, string> = {
  "audio/webm": "webm",
  "video/webm": "webm",
  "audio/mp4": "m4a",
  "audio/x-m4a": "m4a",
  "video/mp4": "m4a",
  "audio/ogg": "ogg",
  "video/ogg": "ogg",
  "audio/wav": "wav",
  "audio/x-wav": "wav",
  "audio/vnd.wave": "wav",
  "audio/mpeg": "mp3"
};

export const MICROPHONE_SECURE_CONTEXT_ERROR =
  "Microphone recording requires HTTPS or localhost. Open this app with an HTTPS URL; remote HTTP/IP addresses cannot show the browser microphone permission prompt.";

export const MICROPHONE_POLICY_ERROR =
  "Microphone access is blocked by this page's browser security policy. If the app is embedded, allow microphone for this frame.";

export type BrowserRecordingEnvironment = {
  isBrowser: boolean;
  isSecureContext?: boolean;
  protocol?: string | null;
  hostname?: string | null;
  hasGetUserMedia: boolean;
  hasMediaRecorder: boolean;
};

const LOCALHOST_HOSTNAMES = new Set(["localhost", "127.0.0.1", "::1", "[::1]"]);

const isLocalhostHostname = (hostname?: string | null): boolean => {
  if (!hostname) {
    return false;
  }

  return LOCALHOST_HOSTNAMES.has(hostname.toLowerCase());
};

const isInsecureRemoteOrigin = (environment: BrowserRecordingEnvironment): boolean => {
  if (!environment.isBrowser) {
    return false;
  }

  if (environment.isSecureContext === false) {
    return true;
  }

  if (environment.isSecureContext === true) {
    return false;
  }

  return environment.protocol === "http:" && !isLocalhostHostname(environment.hostname);
};

export const readBrowserRecordingEnvironment = (): BrowserRecordingEnvironment => {
  if (typeof window === "undefined" || typeof navigator === "undefined") {
    return {
      isBrowser: false,
      hasGetUserMedia: false,
      hasMediaRecorder: false
    };
  }

  const mediaDevices = navigator.mediaDevices;

  return {
    isBrowser: true,
    isSecureContext: window.isSecureContext,
    protocol: window.location.protocol,
    hostname: window.location.hostname,
    hasGetUserMedia: typeof mediaDevices?.getUserMedia === "function",
    hasMediaRecorder: typeof MediaRecorder !== "undefined"
  };
};

export const resolveBrowserRecordingSupportError = (
  environment: BrowserRecordingEnvironment = readBrowserRecordingEnvironment()
): string | null => {
  if (!environment.isBrowser) {
    return "Recording is available only in browser.";
  }

  if (isInsecureRemoteOrigin(environment)) {
    return MICROPHONE_SECURE_CONTEXT_ERROR;
  }

  if (!environment.hasGetUserMedia) {
    return "Your browser does not support microphone recording.";
  }

  if (!environment.hasMediaRecorder) {
    return "Your browser can access the microphone, but it does not support audio recording.";
  }

  return null;
};

export const resolvePreferredAudioMimeType = (
  mediaRecorder: Pick<typeof MediaRecorder, "isTypeSupported"> | null =
    typeof MediaRecorder === "undefined" ? null : MediaRecorder
): string | null => {
  if (!mediaRecorder || typeof mediaRecorder.isTypeSupported !== "function") {
    return null;
  }

  for (const candidate of AUDIO_MIME_CANDIDATES) {
    if (mediaRecorder.isTypeSupported(candidate)) {
      return candidate;
    }
  }

  return null;
};

export const resolveAudioFileExtension = (mimeType: string): string => {
  const baseMime = mimeType.split(";", 1)[0]?.trim().toLowerCase() ?? "";
  const mapped = AUDIO_EXTENSION_BY_MIME[baseMime];
  if (mapped) {
    return mapped;
  }
  if (!baseMime.startsWith("audio/") && !baseMime.startsWith("video/")) {
    return "webm";
  }
  const subtype = baseMime.replace(/^(audio|video)\//, "").replace(/^x-/, "");
  if (subtype === "mpeg") {
    return "mp3";
  }
  if (subtype === "mp4") {
    return "m4a";
  }
  if (subtype === "wave") {
    return "wav";
  }
  const cleaned = subtype.replace(/[^a-z0-9]+/g, "");
  return cleaned && cleaned.length <= 10 ? cleaned : "webm";
};

export const resolveMicrophoneError = (
  error: unknown,
  environment: BrowserRecordingEnvironment = readBrowserRecordingEnvironment()
): string => {
  if (isInsecureRemoteOrigin(environment)) {
    return MICROPHONE_SECURE_CONTEXT_ERROR;
  }

  if (error && typeof error === "object" && "name" in error && typeof (error as { name?: unknown }).name === "string") {
    const name = (error as { name: string }).name;
    if (name === "NotAllowedError" || name === "PermissionDeniedError") {
      return "Microphone access denied. Allow microphone permission in your browser settings.";
    }
    if (name === "SecurityError") {
      return MICROPHONE_POLICY_ERROR;
    }
    if (name === "NotFoundError" || name === "DevicesNotFoundError") {
      return "No microphone found on this device.";
    }
    if (name === "NotReadableError" || name === "TrackStartError") {
      return "Microphone is busy in another app. Close it and try again.";
    }
  }

  return "Cannot access microphone. Check browser permissions and device settings.";
};

export const readBlobAsDataUrl = (blob: Blob): Promise<string> => {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      const result = typeof reader.result === "string" ? reader.result : "";
      if (!result) {
        reject(new Error("Failed to read recorded audio."));
        return;
      }
      resolve(result);
    };
    reader.onerror = () => reject(new Error("Failed to read recorded audio."));
    reader.readAsDataURL(blob);
  });
};
