import DetailsScreen from "../../../src/components/DetailsScreen";
import ProtectedRoute from "../../../src/components/ProtectedRoute";
import { recordingPath } from "../../../src/lib/routes";

export default async function Page({ params }: { params: Promise<{ recordingId: string }> }) {
  const { recordingId } = await params;
  // Next already decodes each dynamic segment; decoding again would corrupt literal % sequences.
  return <ProtectedRoute returnTo={recordingPath(recordingId)}><DetailsScreen recordingId={recordingId} /></ProtectedRoute>;
}
