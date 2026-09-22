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

type CanonicalWebRequest = {
  requestURL: string;
  host?: string | null;
  forwardedHost?: string | null;
  forwardedProto?: string | null;
};

const firstForwardedValue = (value: string | null | undefined): string =>
  value?.split(",", 1)[0]?.trim() ?? "";

const canonicalWebOrigin = (value: string): string => {
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    throw new Error("PUBLIC_WEB_BASE_URL must be an absolute http or https origin.");
  }
  if (
    !["http:", "https:"].includes(url.protocol)
    || url.username
    || url.password
    || (url.pathname !== "" && url.pathname !== "/")
    || url.search
    || url.hash
  ) {
    throw new Error("PUBLIC_WEB_BASE_URL must be an absolute http or https origin.");
  }
  return url.origin;
};

export const resolveCanonicalWebRedirect = (
  request: CanonicalWebRequest,
  configuredBaseURL: string | undefined,
): string | null => {
  const requestURL = new URL(request.requestURL);
  if (requestURL.pathname === "/web-healthz") return null;

  const configured = configuredBaseURL?.trim();
  if (!configured) return null;

  const canonicalOrigin = canonicalWebOrigin(configured);
  const host = firstForwardedValue(request.forwardedHost)
    || request.host?.trim()
    || requestURL.host;
  const protocol = (firstForwardedValue(request.forwardedProto) || requestURL.protocol)
    .replace(/:$/, "")
    .toLowerCase();

  let requestOrigin = "";
  try {
    requestOrigin = new URL(`${protocol}://${host}`).origin;
  } catch {
    // A malformed forwarded origin must not influence the fixed redirect target.
  }
  if (requestOrigin === canonicalOrigin) return null;

  const destination = new URL(canonicalOrigin);
  destination.pathname = requestURL.pathname;
  destination.search = requestURL.search;
  return destination.href;
};
