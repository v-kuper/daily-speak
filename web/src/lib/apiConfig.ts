export const resolvePublicApiBaseUrl = (
  value: string | undefined,
  environment: string | undefined,
): string => {
  const configured = value?.trim();
  if (!configured) {
    if (environment === "production") {
      throw new Error("PUBLIC_API_BASE_URL is required in production.");
    }
    return "http://localhost:3219";
  }

  let url: URL;
  try {
    url = new URL(configured);
  } catch {
    throw new Error("PUBLIC_API_BASE_URL must be an absolute http or https URL.");
  }
  if (!/^https?:\/\//i.test(configured) || !["http:", "https:"].includes(url.protocol)) {
    throw new Error("PUBLIC_API_BASE_URL must be an absolute http or https URL.");
  }
  if (url.username || url.password || url.search || url.hash) {
    throw new Error("PUBLIC_API_BASE_URL must not contain credentials, a query, or a fragment.");
  }
  return url.toString().replace(/\/+$/, "");
};
