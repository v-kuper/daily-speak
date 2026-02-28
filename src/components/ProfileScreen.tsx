"use client";

import { useEffect, useState } from "react";
import {
  cancelSubscription,
  fetchOllamaModelSettings,
  openInterests,
  saveEnglishLevel,
  saveOllamaModel,
  subscribeMonthly
} from "../store/slices/appSlice";
import { ENGLISH_LEVEL_OPTIONS, parseEnglishLevel } from "../lib/englishLevel";
import { useAppDispatch, useAppSelector } from "../store/hooks";
import { formatTime } from "../lib/utils";

export default function ProfileScreen() {
  const dispatch = useAppDispatch();
  const {
    userEmail,
    selectedInterestIds,
    isSubscriber,
    weeklyLimitSeconds,
    weeklyUsedSeconds,
    weeklyRemainingSeconds,
    subscriptionExpiresAt,
    subscriptionCancelled,
    subscriptionActionStatus,
    subscriptionActionError,
    selectedEnglishLevel,
    englishLevelSaveStatus,
    englishLevelSaveError,
    selectedOllamaModel,
    availableOllamaModels,
    ollamaModelsStatus,
    ollamaModelsError,
    ollamaModelSaveStatus,
    ollamaModelSaveError
  } = useAppSelector((state) => state.app);
  const freeLimit = Math.max(0, weeklyLimitSeconds ?? 0);
  const freeUsed = Math.max(0, weeklyUsedSeconds);
  const freeRemaining = Math.max(0, weeklyRemainingSeconds ?? 0);
  const isSubscriptionLoading = subscriptionActionStatus === "loading";
  const isModelsLoading = ollamaModelsStatus === "loading";
  const isModelSaving = ollamaModelSaveStatus === "loading";
  const subscriptionEndsLabel = subscriptionExpiresAt
    ? new Date(subscriptionExpiresAt).toLocaleString("ru-RU", {
        day: "2-digit",
        month: "long",
        year: "numeric"
      })
    : null;
  const [modelDraft, setModelDraft] = useState(selectedOllamaModel);
  const [englishLevelDraft, setEnglishLevelDraft] = useState(selectedEnglishLevel);

  useEffect(() => {
    setModelDraft(selectedOllamaModel);
  }, [selectedOllamaModel]);

  useEffect(() => {
    setEnglishLevelDraft(selectedEnglishLevel);
  }, [selectedEnglishLevel]);

  useEffect(() => {
    if (ollamaModelsStatus === "idle") {
      void dispatch(fetchOllamaModelSettings());
    }
  }, [dispatch, ollamaModelsStatus]);

  const canSaveModel =
    modelDraft.trim().length > 0 &&
    modelDraft.trim() !== selectedOllamaModel &&
    !isModelsLoading &&
    !isModelSaving;
  const isEnglishLevelSaving = englishLevelSaveStatus === "loading";
  const canSaveEnglishLevel = englishLevelDraft !== selectedEnglishLevel && !isEnglishLevelSaving;

  return (
    <section className="profile-screen">
      <h2>Профиль</h2>

      <div className="profile-card">
        <div className="profile-row">
          <div className="profile-label">Email</div>
          <div className="profile-value">{userEmail}</div>
        </div>

        <div className="profile-row">
          <div className="profile-label">Plan</div>
          <div className="profile-value">
            {isSubscriber ? "Subscriber: unlimited per week (max 10:00 per session)." : "Free: 10:00 per week."}
          </div>
        </div>

        {!isSubscriber && (
          <div className="profile-row">
            <div className="profile-label">Weekly usage</div>
            <div className="profile-value">
              {formatTime(freeUsed)} / {formatTime(freeLimit)} used, {formatTime(freeRemaining)} left
            </div>
          </div>
        )}

        <div className="profile-row">
          <div className="profile-label">Подписка</div>
          <div className="profile-value">
            {isSubscriber
              ? subscriptionCancelled
                ? `Отменена, но Pro активен до ${subscriptionEndsLabel ?? "даты окончания"}`
                : `Активна до ${subscriptionEndsLabel ?? "даты окончания"}`
              : "Неактивна"}
          </div>
        </div>

        {subscriptionActionError && <div className="auth-error">{subscriptionActionError}</div>}

        <div className="auth-buttons">
          <button
            className="btn btn-primary"
            onClick={() => void dispatch(subscribeMonthly())}
            disabled={isSubscriptionLoading}
          >
            {isSubscriptionLoading ? "Подождите..." : isSubscriber ? "Продлить на месяц" : "Оформить на месяц"}
          </button>
          {isSubscriber && !subscriptionCancelled && (
            <button
              className="btn btn-secondary"
              onClick={() => void dispatch(cancelSubscription())}
              disabled={isSubscriptionLoading}
            >
              {isSubscriptionLoading ? "Подождите..." : "Отменить подписку"}
            </button>
          )}
        </div>

        <div className="profile-row">
          <div className="profile-label">Ollama model</div>
          <select
            className="profile-select"
            value={modelDraft}
            onChange={(event) => setModelDraft(event.target.value)}
            disabled={isModelsLoading || isModelSaving || availableOllamaModels.length === 0}
          >
            {availableOllamaModels.map((modelName) => (
              <option key={modelName} value={modelName}>
                {modelName}
              </option>
            ))}
          </select>
        </div>

        {ollamaModelsError && <div className="auth-error">{ollamaModelsError}</div>}
        {ollamaModelSaveError && <div className="auth-error">{ollamaModelSaveError}</div>}

        <div className="auth-buttons">
          <button
            className="btn btn-secondary"
            onClick={() => void dispatch(fetchOllamaModelSettings())}
            disabled={isModelsLoading || isModelSaving}
          >
            {isModelsLoading ? "Обновляем..." : "Обновить модели"}
          </button>
          <button
            className="btn btn-primary"
            onClick={() => void dispatch(saveOllamaModel(modelDraft))}
            disabled={!canSaveModel}
          >
            {isModelSaving ? "Сохраняем..." : "Сохранить модель"}
          </button>
        </div>

        <div className="profile-row">
          <div className="profile-label">English level</div>
          <select
            className="profile-select"
            value={englishLevelDraft}
            onChange={(event) => {
              const level = parseEnglishLevel(event.target.value);
              if (level) {
                setEnglishLevelDraft(level);
              }
            }}
            disabled={isEnglishLevelSaving}
          >
            {ENGLISH_LEVEL_OPTIONS.map((levelOption) => (
              <option key={levelOption.value} value={levelOption.value}>
                {levelOption.label}
              </option>
            ))}
          </select>
          <div className="profile-value">
            {ENGLISH_LEVEL_OPTIONS.find((option) => option.value === englishLevelDraft)?.description}
          </div>
        </div>

        {englishLevelSaveError && <div className="auth-error">{englishLevelSaveError}</div>}

        <div className="auth-buttons">
          <button
            className="btn btn-primary"
            onClick={() => void dispatch(saveEnglishLevel(englishLevelDraft))}
            disabled={!canSaveEnglishLevel}
          >
            {isEnglishLevelSaving ? "Сохраняем..." : "Сохранить уровень"}
          </button>
        </div>

        <button className="btn btn-secondary" onClick={() => dispatch(openInterests())}>
          Мои интересы ({selectedInterestIds.length})
        </button>
      </div>
    </section>
  );
}
