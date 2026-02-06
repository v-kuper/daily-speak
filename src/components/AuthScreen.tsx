"use client";

import { FormEvent } from "react";
import { useAppDispatch, useAppSelector } from "../store/hooks";
import {
  backToEmailStep,
  cancelAuth,
  setAuthCodeDraft,
  setAuthEmailDraft,
  submitAuthEmail,
  verifyAuthCode
} from "../store/slices/appSlice";

export default function AuthScreen() {
  const dispatch = useAppDispatch();
  const { authStep, authEmailDraft, authCodeDraft, authPendingEmail, authError, pendingSaveAfterAuth } =
    useAppSelector((state) => state.app);

  const onSubmitEmail = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    dispatch(submitAuthEmail());
  };

  const onSubmitCode = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    dispatch(verifyAuthCode());
  };

  return (
    <section className="auth-screen">
      <h2>{pendingSaveAfterAuth ? "Sign in to save recording" : "Sign in / Register"}</h2>

      {authStep === "email" ? (
        <form className="auth-form" onSubmit={onSubmitEmail}>
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
          />

          {authError && <div className="auth-error">{authError}</div>}

          <div className="auth-buttons">
            <button type="button" className="btn btn-secondary" onClick={() => dispatch(cancelAuth())}>
              Back
            </button>
            <button type="submit" className="btn btn-primary">
              Continue
            </button>
          </div>
        </form>
      ) : (
        <form className="auth-form" onSubmit={onSubmitCode}>
          <div className="auth-hint">Code was sent to {authPendingEmail}</div>
          <div className="auth-hint">Mock code: 123456</div>

          <label htmlFor="auth-code" className="auth-label">
            Verification code
          </label>
          <input
            id="auth-code"
            type="text"
            inputMode="numeric"
            placeholder="123456"
            maxLength={6}
            value={authCodeDraft}
            onChange={(event) => dispatch(setAuthCodeDraft(event.target.value.replace(/\s/g, "")))}
          />

          {authError && <div className="auth-error">{authError}</div>}

          <div className="auth-buttons">
            <button type="button" className="btn btn-secondary" onClick={() => dispatch(backToEmailStep())}>
              Change email
            </button>
            <button type="submit" className="btn btn-primary">
              Confirm
            </button>
          </div>
        </form>
      )}
    </section>
  );
}
