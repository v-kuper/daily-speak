import DetailsScreen from "../../../src/components/DetailsScreen";
import ProtectedRoute from "../../../src/components/ProtectedRoute";
import { recordingPath } from "../../../src/lib/routes";

export default async function Page({ params }: { params: Promise<{ recordingId: string }> }) {
  const { recordingId } = await params;
  return <ProtectedRoute returnTo={recordingPath(recordingId)}><DetailsScreen /></ProtectedRoute>;
}
