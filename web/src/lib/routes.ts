const STATIC_RETURN_ROUTES = new Set([
  "/speak",
  "/history",
  "/profile",
  "/profile/subscription",
  "/profile/english-level",
  "/profile/interests"
]);

export const parseHistoryDate = (value: unknown): string | null => {
  if (typeof value !== "string" || !/^\d{4}-\d{2}-\d{2}$/.test(value) || value.length !== 10) {
    return null;
  }
  const date = new Date(`${value}T00:00:00.000Z`);
  return Number.isFinite(date.getTime()) && date.toISOString().slice(0, 10) === value ? value : null;
};

export const recordingPath = (recordingId: string): string => `/history/${encodeURIComponent(recordingId)}`;

export const safeReturnTo = (value: unknown): string => {
  if (typeof value !== "string" || !value.startsWith("/") || value.startsWith("//")) {
    return "/speak";
  }
  const [path, rawQuery = "", ...extra] = value.split("?");
  if (extra.length > 0 || value.includes("#")) {
    return "/speak";
  }
  if (STATIC_RETURN_ROUTES.has(path) && rawQuery === "") {
    return path;
  }
  if (path === "/history" && rawQuery !== "") {
    const params = new URLSearchParams(rawQuery);
    const date = parseHistoryDate(params.get("date"));
    if (params.size === 1 && !rawQuery.includes("&") && date) {
      return `/history?date=${date}`;
    }
  }
  return rawQuery === "" && /^\/history\/[A-Za-z0-9-]+$/.test(path) ? path : "/speak";
};

export const protectedRouteDestination = (
  authInitialized: boolean,
  isAuthenticated: boolean,
  returnTo: unknown
): string | null =>
  !authInitialized || isAuthenticated ? null : `/auth?returnTo=${encodeURIComponent(safeReturnTo(returnTo))}`;

export const claimRouteRedirect = (issued: { current: string | null }, destination: string | null): boolean => {
  const shouldRedirect = destination !== null && issued.current !== destination;
  issued.current = destination;
  return shouldRedirect;
};
