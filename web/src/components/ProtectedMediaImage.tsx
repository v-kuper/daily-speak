"use client";

import { useProtectedMediaURL } from "../lib/useProtectedMediaURL";

export default function ProtectedMediaImage({
  downloadPath,
  localURL,
  alt,
  className,
}: {
  downloadPath: string | null;
  localURL?: string | null;
  alt: string;
  className: string;
}) {
  const media = useProtectedMediaURL(downloadPath);
  const source = media.url ?? (downloadPath ? null : localURL ?? null);

  if (!source) {
    return null;
  }

  return (
    // Signed, expiring media URLs cannot use Next's build-time image optimization pipeline.
    // eslint-disable-next-line @next/next/no-img-element
    <img
      src={source}
      alt={alt}
      className={className}
      onLoad={downloadPath ? media.reportReady : undefined}
      onError={downloadPath ? media.reportError : undefined}
    />
  );
}
