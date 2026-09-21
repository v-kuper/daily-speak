"use client";

import { useEffect, type ReactNode } from "react";
import { useRouter } from "next/navigation";
import { protectedRouteDestination } from "../lib/routes";
import { useAppSelector } from "../store/hooks";

export default function ProtectedRoute({ children, returnTo }: { children: ReactNode; returnTo: string }) {
  const router = useRouter();
  const { authInitialized, isAuthenticated } = useAppSelector((state) => state.app);

  useEffect(() => {
    const currentRoute = typeof window === "undefined" ? returnTo : window.location.pathname + window.location.search;
    const destination = protectedRouteDestination(authInitialized, isAuthenticated, currentRoute);
    if (destination) {
      router.replace(destination);
    }
  }, [authInitialized, isAuthenticated, returnTo, router]);

  if (!authInitialized || !isAuthenticated) {
    return <p role="status">Loading session...</p>;
  }
  return children;
}
