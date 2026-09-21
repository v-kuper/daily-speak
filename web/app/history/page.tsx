import { Suspense } from "react";
import HistoryScreen from "../../src/components/HistoryScreen";
import ProtectedRoute from "../../src/components/ProtectedRoute";

export default function Page() {
  return <ProtectedRoute returnTo="/history"><Suspense fallback={<p role="status">Loading history...</p>}><HistoryScreen /></Suspense></ProtectedRoute>;
}
