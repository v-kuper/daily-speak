/** Both recording paths share these audio tracks. Muting emits silence without
 * stopping the recorder, realtime connection, or the current answer. */
export class InterviewMicrophone {
  private stream: Pick<MediaStream, "getAudioTracks"> | null = null;
  private userMuted = false;
  private questionSpeechMuted = false;

  attach(stream: Pick<MediaStream, "getAudioTracks"> | null): void {
    this.stream = stream;
    this.apply();
  }

  toggleUserMuted(): boolean {
    this.userMuted = !this.userMuted;
    this.apply();
    return this.userMuted;
  }

  setQuestionSpeechMuted(muted: boolean): void {
    this.questionSpeechMuted = muted;
    this.apply();
  }

  reset(): void {
    this.userMuted = false;
    this.questionSpeechMuted = false;
    this.apply();
  }

  private apply(): void {
    for (const track of this.stream?.getAudioTracks() ?? []) {
      track.enabled = !(this.userMuted || this.questionSpeechMuted);
    }
  }
}
