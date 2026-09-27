export type ApiClient = {
  fetch: (path: string, init?: RequestInit) => Promise<Response>;
  url: (path: string) => string;
};

export class ApiUnavailableError extends Error {
  constructor() {
    super("The API is temporarily unavailable.");
    this.name = "ApiUnavailableError";
  }
}

export const createApiClient = (baseURL: string, fetchImpl: typeof fetch = fetch): ApiClient => {
  const normalizedBaseURL = baseURL.replace(/\/+$/, "");
  const resolveURL = (path: string): string => {
    if (/^https?:\/\//i.test(path)) return path;
    const normalizedPath = path.startsWith("/") ? path : `/${path}`;
    return `${normalizedBaseURL}${normalizedPath}`;
  };
  return {
    async fetch(path, init = {}) {
      if (/^https?:\/\//i.test(path)) {
        throw new Error("API requests must use a relative path.");
      }
      try {
        return await fetchImpl(resolveURL(path), {
          ...init,
          credentials: "include",
        });
      } catch (error) {
        if (error && typeof error === "object" && "name" in error && error.name === "AbortError") {
          throw error;
        }
        throw new ApiUnavailableError();
      }
    },
    url: resolveURL,
  };
};

export const readApiJSON = async <T>(response: Response): Promise<T | null> => {
  const body = await response.text();
  if (!body) return null;
  try {
    return JSON.parse(body) as T;
  } catch {
    throw new Error("The API returned an invalid response.");
  }
};

let configuredClient: ApiClient | null = null;
type AuthorizationProvider = (forceRefresh: boolean, rejectedToken?: string) => Promise<string | null>;
let authorizationProvider: AuthorizationProvider | null = null;

export const configureApiClient = (baseURL: string): void => {
  configuredClient = createApiClient(baseURL);
};

export const configureApiAuthorization = (provider: AuthorizationProvider | null): void => {
  authorizationProvider = provider;
};

const getApiClient = (): ApiClient => {
  if (!configuredClient) {
    throw new Error("The API client is not configured. Initialize it in Providers before making requests.");
  }
  return configuredClient;
};

export const apiFetchUnauthenticated = (path: string, init?: RequestInit): Promise<Response> => getApiClient().fetch(path, init);

export const apiFetch = (path: string, init: RequestInit = {}): Promise<Response> => {
  const client = getApiClient();
  return (async () => {
    const headers: Record<string, string> = {};
    new Headers(init.headers).forEach((value, name) => {
      const originalName = init.headers && !(init.headers instanceof Headers) && !Array.isArray(init.headers)
        ? Object.keys(init.headers).find((candidate) => candidate.toLowerCase() === name.toLowerCase())
        : undefined;
      headers[originalName ?? name] = value;
    });
    const authorizationName = Object.keys(headers).find((name) => name.toLowerCase() === "authorization");
    const hasExplicitAuthorization = authorizationName !== undefined;
    let accessToken: string | null = null;
    if (!hasExplicitAuthorization && authorizationProvider) {
      accessToken = await authorizationProvider(false);
      if (accessToken) headers.Authorization = `Bearer ${accessToken}`;
    }
    let response = await client.fetch(path, { ...init, headers });
    if (response.status !== 401 || hasExplicitAuthorization || !accessToken || !authorizationProvider) {
      return response;
    }
    const refreshed = await authorizationProvider(true, accessToken);
    if (!refreshed) return response;
    headers.Authorization = `Bearer ${refreshed}`;
    response = await client.fetch(path, { ...init, headers });
    return response;
  })();
};

export const resolveApiURL = (path: string): string => getApiClient().url(path);
