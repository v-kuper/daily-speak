import { Suspense } from "react";
import AuthRoute from "../../src/components/AuthRoute";

export default function Page() {
  return <Suspense fallback={<p role="status">Loading...</p>}><AuthRoute /></Suspense>;
}
