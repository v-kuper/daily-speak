import InterestsScreen from "../../../src/components/InterestsScreen";
import ProtectedRoute from "../../../src/components/ProtectedRoute";

export default function Page() {
  return <ProtectedRoute returnTo="/profile/interests"><InterestsScreen /></ProtectedRoute>;
}
