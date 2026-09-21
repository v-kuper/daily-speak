export type ApiClient = {
  fetch: (path: string, init?: RequestInit) => Promise<Response>;
  assetURL: (value: string | null) => string | null;
};

export class ApiUnavailableError extends Error {
  constructor() {
    super("The API is temporarily unavailable.");
    this.name = "ApiUnavailableError";
  }
}

export const createApiClient = (baseURL: string, fetchImpl: typeof fetch = fetch): ApiClient => {
  const normalizedBaseURL = baseURL.replace(/\/+$/, "");
  return {
    async fetch(path, init = {}) {
      const normalizedPath = path.startsWith("/") ? path : `/${path}`;
      try {
        return await fetchImpl(`${normalizedBaseURL}${normalizedPath}`, {
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
    assetURL(value) {
      return value?.startsWith("/uploads/") ? `${normalizedBaseURL}${value}` : value;
    },
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

export const configureApiClient = (baseURL: string): void => {
  configuredClient = createApiClient(baseURL);
};

const getApiClient = (): ApiClient => {
  if (!configuredClient) {
    throw new Error("The API client is not configured. Initialize it in Providers before making requests.");
  }
  return configuredClient;
};

export const apiFetch = (path: string, init?: RequestInit): Promise<Response> => getApiClient().fetch(path, init);

export const resolveApiAssetURL = (value: string | null): string | null => getApiClient().assetURL(value);
