"use client";

import { useRef, type ReactNode } from "react";
import { Provider } from "react-redux";
import { makeStore, type AppStore } from "../src/store";
import { configureApiClient } from "../src/lib/apiClient";

type ProvidersProps = {
  children: ReactNode;
  apiBaseURL: string;
};

export default function Providers({ children, apiBaseURL }: ProvidersProps) {
  const storeRef = useRef<AppStore | null>(null);

  if (!storeRef.current) {
    configureApiClient(apiBaseURL);
    storeRef.current = makeStore();
  }

  return <Provider store={storeRef.current}>{children}</Provider>;
}
