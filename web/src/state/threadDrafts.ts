/**
 * Unsent message text per conversation.
 *
 * The visible composer owns its text; a thread's draft is parked here while
 * the user is elsewhere and taken back on return. Each thread also has an
 * edit revision that only typing advances (navigation never does), so a
 * completed send can tell whether the thread still holds the version that
 * was sent, wherever that text currently lives.
 */
export class ThreadDraftStore {
  readonly #drafts = new Map<string, string>()
  readonly #revisions = new Map<string, number>()

  /** Park the composer text of a thread the user is leaving. */
  park(threadKey: string, text: string): void {
    if (!threadKey) return
    if (text.trim()) this.#drafts.set(threadKey, text)
    else this.#drafts.delete(threadKey)
  }

  /** Hand a parked draft back to the composer; the composer owns it again. */
  take(threadKey: string): string {
    if (!threadKey) return ''
    const text = this.#drafts.get(threadKey) || ''
    this.#drafts.delete(threadKey)
    return text
  }

  /** The user edited this thread's draft. */
  edited(threadKey: string): void {
    if (threadKey) this.#revisions.set(threadKey, this.revision(threadKey) + 1)
  }

  revision(threadKey: string): number {
    return this.#revisions.get(threadKey) || 0
  }

  /**
   * A send captured at `sentRevision` succeeded. Returns true when the thread
   * still holds that version, and forgets it if parked; the caller clears it
   * when it is the visible composer. A later edit keeps the newer draft.
   */
  settleSent(threadKey: string, sentRevision: number): boolean {
    if (!threadKey || this.revision(threadKey) !== sentRevision) return false
    this.#drafts.delete(threadKey)
    return true
  }

  peek(threadKey: string): string {
    return this.#drafts.get(threadKey) || ''
  }
}
