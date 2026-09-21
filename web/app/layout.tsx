import type { Metadata } from "next";
import type { ReactNode } from "react";
import Providers from "./providers";
import AppShell from "../src/components/AppShell";
import { resolvePublicApiBaseUrl } from "../src/lib/apiConfig";
import "./globals.css";

export const dynamic = "force-dynamic";

export const metadata: Metadata = {
  title: "Daily Speaking Practice",
  description: "Demo app rewritten with Next.js and Redux Toolkit"
};

type RootLayoutProps = {
  children: ReactNode;
};

export default function RootLayout({ children }: RootLayoutProps) {
  const apiBaseURL = resolvePublicApiBaseUrl(process.env.PUBLIC_API_BASE_URL, process.env.NODE_ENV);
  return (
    <html lang="en">
      <body>
        <Providers apiBaseURL={apiBaseURL}><AppShell>{children}</AppShell></Providers>
      </body>
    </html>
  );
}
