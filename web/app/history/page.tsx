import HistoryScreen from "../../src/components/HistoryScreen";
import ProtectedRoute from "../../src/components/ProtectedRoute";

export default function Page() {
  return <ProtectedRoute returnTo="/history"><HistoryScreen /></ProtectedRoute>;
}
