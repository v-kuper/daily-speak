"use client";

import { useSearchParams } from "next/navigation";
import { safeReturnTo } from "../lib/routes";
import AuthScreen from "./AuthScreen";

export default function AuthRoute() {
  const searchParams = useSearchParams();
  return <AuthScreen returnTo={safeReturnTo(searchParams.get("returnTo"))} />;
}
