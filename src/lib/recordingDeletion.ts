type RecordingLike = {
  id: string;
};

type FeedPostLike = {
  sourceRecordingId: string;
};

export const filterDeletedRecordings = <R extends RecordingLike>(recordings: R[], recordingIds: string[]): R[] => {
  const deletedIds = new Set(recordingIds);
  return recordings.filter((recording) => !deletedIds.has(recording.id));
};

export const filterDeletedFeedPosts = <F extends FeedPostLike>(feedPosts: F[], recordingIds: string[]): F[] => {
  const deletedIds = new Set(recordingIds);
  return feedPosts.filter((post) => !deletedIds.has(post.sourceRecordingId));
};

export const removeRecordingAndFeedPost = <R extends RecordingLike, F extends FeedPostLike>(
  recordings: R[],
  feedPosts: F[],
  recordingId: string
): { recordings: R[]; feedPosts: F[] } => {
  return {
    recordings: filterDeletedRecordings(recordings, [recordingId]),
    feedPosts: filterDeletedFeedPosts(feedPosts, [recordingId])
  };
};
