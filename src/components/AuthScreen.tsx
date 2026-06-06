"use client";

import { FormEvent } from "react";
import { useAppDispatch, useAppSelector } from "../store/hooks";
import {
  cancelAuth,
  setAuthEmailDraft,
  setAuthPasswordDraft,
  signIn,
  signUp
} from "../store/slices/appSlice";

export default function AuthScreen() {
  const dispatch = useAppDispatch();
  const { authEmailDraft, authPasswordDraft, authError, authStatus, pendingSaveAfterAuth } = useAppSelector(
    (state) => state.app
  );
  const isLoading = authStatus === "loading";

  const onSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    void dispatch(signIn());
  };

  const onRegister = () => {
    void dispatch(signUp());
  };

  return (
    <section className="screen-section auth-screen">
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

        <div className="auth-buttons">
          <button type="button" className="btn btn-secondary" onClick={() => dispatch(cancelAuth())} disabled={isLoading}>
            Back
          </button>
          <button type="submit" className="btn btn-primary" disabled={isLoading}>
            {isLoading ? "Please wait..." : "Sign in"}
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
