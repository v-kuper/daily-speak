import ProfileScreen from "../../../src/components/ProfileScreen";
import ProtectedRoute from "../../../src/components/ProtectedRoute";

export default function Page() {
  return <ProtectedRoute returnTo="/profile/interests"><ProfileScreen /></ProtectedRoute>;
}
