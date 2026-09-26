import GuestPreviewScreen from "../../../src/components/GuestPreviewScreen";

export default async function Page({ params }: { params: Promise<{ previewId: string }> }) {
  const { previewId } = await params;
  return <GuestPreviewScreen previewId={previewId} />;
}
