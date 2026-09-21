type RecordingLike = {
  id: string;
};

export const filterDeletedRecordings = <R extends RecordingLike>(recordings: R[], recordingIds: string[]): R[] => {
  const deletedIds = new Set(recordingIds);
  return recordings.filter((recording) => !deletedIds.has(recording.id));
};

export const removeRecording = <R extends RecordingLike>(recordings: R[], recordingId: string): R[] => {
  return filterDeletedRecordings(recordings, [recordingId]);
};
