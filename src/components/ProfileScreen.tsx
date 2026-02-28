"use client";

import { cancelSubscription, openInterests, subscribeMonthly } from "../store/slices/appSlice";
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
    subscriptionActionError
  } = useAppSelector((state) => state.app);
  const freeLimit = Math.max(0, weeklyLimitSeconds ?? 0);
  const freeUsed = Math.max(0, weeklyUsedSeconds);
  const freeRemaining = Math.max(0, weeklyRemainingSeconds ?? 0);
  const isSubscriptionLoading = subscriptionActionStatus === "loading";
  const subscriptionEndsLabel = subscriptionExpiresAt
    ? new Date(subscriptionExpiresAt).toLocaleString("ru-RU", {
        day: "2-digit",
        month: "long",
        year: "numeric"
      })
    : null;

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

        <button className="btn btn-secondary" onClick={() => dispatch(openInterests())}>
          Мои интересы ({selectedInterestIds.length})
        </button>
      </div>
    </section>
  );
}
