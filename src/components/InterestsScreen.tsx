"use client";

import {
  backToProfile,
  INTEREST_OPTIONS,
  MAX_SELECTED_INTERESTS,
  toggleInterest
} from "../store/slices/appSlice";
import { useAppDispatch, useAppSelector } from "../store/hooks";

export default function InterestsScreen() {
  const dispatch = useAppDispatch();
  const { selectedInterestIds } = useAppSelector((state) => state.app);
  const selectedCount = selectedInterestIds.length;

  return (
    <section className="profile-screen">
      <button className="back-btn" onClick={() => dispatch(backToProfile())}>
        ← Назад в профиль
      </button>
      <h2>Мои интересы</h2>

      <div className="section">
        <p className="profile-subtitle">
          Выберите до {MAX_SELECTED_INTERESTS}. Выбрано: {selectedCount}
        </p>

        <div className="interest-chips">
          {INTEREST_OPTIONS.map((interest) => {
            const isSelected = selectedInterestIds.includes(interest.id);
            const isDisabled = !isSelected && selectedCount >= MAX_SELECTED_INTERESTS;

            return (
              <button
                key={interest.id}
                className={`interest-chip ${isSelected ? "active" : ""}`}
                onClick={() => dispatch(toggleInterest(interest.id))}
                disabled={isDisabled}
              >
                <span className="interest-chip-emoji" aria-hidden="true">
                  {interest.emoji}
                </span>
                <span>{interest.label}</span>
              </button>
            );
          })}
        </div>
      </div>
    </section>
  );
}
