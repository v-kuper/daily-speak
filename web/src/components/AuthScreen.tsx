"use client";

import { FormEvent } from "react";
import { useRouter } from "next/navigation";
import { authenticateAndNavigate, cancelAuthentication } from "../lib/routeFlows";
import { useAppDispatch, useAppSelector, useAppStore } from "../store/hooks";
import {
  setAuthEmailDraft,
  setAuthPasswordDraft,
} from "../store/slices/appSlice";

export default function AuthScreen({ returnTo }: { returnTo: string }) {
  const dispatch = useAppDispatch();
  const store = useAppStore();
  const router = useRouter();
  const { authEmailDraft, authPasswordDraft, authError, authStatus, pendingSaveAfterAuth, recordingSaveStatus, recordingSaveError, isAuthenticated } = useAppSelector(
    (state) => state.app
  );
  const isLoading = authStatus === "loading" || recordingSaveStatus === "loading";
  const retryingSave = isAuthenticated && pendingSaveAfterAuth;

  const onSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    void authenticateAndNavigate(store, router, "signIn", returnTo);
  };

  const onRegister = () => {
    void authenticateAndNavigate(store, router, "signUp", returnTo);
  };

  return (
    <section className="auth-screen">
      <h2>{pendingSaveAfterAuth ? "Sign in to save recording" : "Sign in / Register"}</h2>

      <form className="auth-form" onSubmit={onSubmit}>
        <label htmlFor="auth-email" className="auth-label">
          Email
        </label>
        <input
          id="auth-email"
          type="email"
          autoComplete="email"
          placeholder="name@example.com"
          value={authEmailDraft}
          onChange={(event) => dispatch(setAuthEmailDraft(event.target.value))}
          disabled={isLoading}
        />

        <label htmlFor="auth-password" className="auth-label">
          Password
        </label>
        <input
          id="auth-password"
          type="password"
          autoComplete="current-password"
          placeholder="At least 8 characters"
          value={authPasswordDraft}
          onChange={(event) => dispatch(setAuthPasswordDraft(event.target.value))}
          disabled={isLoading}
        />

        {authError && <div className="auth-error">{authError}</div>}
        {recordingSaveError && <div className="auth-error" role="alert">{recordingSaveError}</div>}

        <div className="auth-buttons">
          <button type="button" className="btn btn-secondary" onClick={() => cancelAuthentication(store, router)} disabled={isLoading}>
            Back
          </button>
          <button type="submit" className="btn btn-primary" disabled={isLoading}>
            {isLoading ? "Please wait..." : retryingSave ? "Retry save" : "Sign in"}
          </button>
        </div>
        <button
          type="button"
          className="btn btn-secondary btn-large auth-create-btn"
          onClick={onRegister}
          disabled={isLoading}
        >
          {isLoading ? "Please wait..." : "Create account"}
        </button>
      </form>
    </section>
  );
}
