"use client";

import { openInterests } from "../store/slices/appSlice";
import { useAppDispatch, useAppSelector } from "../store/hooks";

export default function ProfileScreen() {
  const dispatch = useAppDispatch();
  const { userEmail, selectedInterestIds } = useAppSelector((state) => state.app);

  return (
    <section className="profile-screen">
      <h2>Профиль</h2>

      <div className="profile-card">
        <div className="profile-row">
          <div className="profile-label">Email</div>
          <div className="profile-value">{userEmail}</div>
        </div>

        <button className="btn btn-secondary" onClick={() => dispatch(openInterests())}>
          Мои интересы ({selectedInterestIds.length})
        </button>
      </div>
    </section>
  );
}
