"use client";

import { useEffect, type ReactNode } from "react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useAppDispatch, useAppSelector } from "../store/hooks";
import { fetchUserData, logout, restoreSession } from "../store/slices/appSlice";

export default function AppShell({ children }: { children: ReactNode }) {
  const dispatch = useAppDispatch();
  const pathname = usePathname();
  const router = useRouter();
  const { isAuthenticated, userEmail, authInitialized, authStatus, userDataStatus, pendingSaveAfterAuth } =
    useAppSelector((state) => state.app);

  useEffect(() => {
    if (!authInitialized && authStatus !== "loading") {
      void dispatch(restoreSession());
    }
  }, [authInitialized, authStatus, dispatch]);

  useEffect(() => {
    if (isAuthenticated && !pendingSaveAfterAuth && userDataStatus === "idle") {
      void dispatch(fetchUserData());
    }
  }, [dispatch, isAuthenticated, pendingSaveAfterAuth, userDataStatus]);

  const onLogout = async () => {
    await dispatch(logout());
    router.replace("/speak");
  };
  const historyActive = pathname === "/history" || pathname.startsWith("/history/");

  return (
    <div className="app-container">
      <header className="header">
        <Link className="brand-title" href="/speak">Daily Speaking</Link>
        <div className="header-actions">
          <ul className="nav-tabs" aria-label="Main navigation">
            <li>
              <Link className={pathname === "/speak" ? "active" : ""} aria-current={pathname === "/speak" ? "page" : undefined} href="/speak">
                Speak
              </Link>
            </li>
            {isAuthenticated && (
              <li>
                <Link className={historyActive ? "active" : ""} aria-current={historyActive ? "page" : undefined} href="/history">
                  History
                </Link>
              </li>
            )}
          </ul>
          {isAuthenticated ? (
            <div className="session-info">
              <Link className="session-email-btn" href="/profile">{userEmail}</Link>
              <button type="button" className="btn btn-secondary btn-small" onClick={() => void onLogout()} disabled={authStatus === "loading"}>
                Log out
              </button>
            </div>
          ) : (
            <Link className="btn btn-secondary btn-small" href="/auth">Sign in / Register</Link>
          )}
        </div>
      </header>
      <main className="main-content">{children}</main>
    </div>
  );
}
