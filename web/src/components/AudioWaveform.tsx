import type { CSSProperties } from "react";

type AudioWaveformProps = {
  variant?: "compact" | "hero";
  active?: boolean;
};

const WAVEFORM_BARS = [18, 34, 54, 42, 28, 64, 48, 26, 58, 36, 22, 44, 30, 20];

export default function AudioWaveform({ variant = "compact", active = false }: AudioWaveformProps) {
  return (
    <div className={`audio-waveform audio-waveform-${variant} ${active ? "active" : ""}`} aria-hidden="true">
      {WAVEFORM_BARS.map((height, index) => (
        <span key={`${height}-${index}`} style={{ "--bar-height": `${height}%` } as CSSProperties} />
      ))}
    </div>
  );
}
