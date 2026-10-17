/**
 * Unsent message text for conversations that are not on screen.
 *
 * The visible composer owns its text; a thread's draft is parked here only
 * while the user is elsewhere (another thread or the new-message composer)
 * and handed back when they return. A successful send settles the thread so
 * the sent text can never be restored, while text typed after the send began
 * is kept.
 */
export class ThreadDraftStore {
  readonly #drafts = new Map<string, string>()

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

  /**
   * `sentText` from `threadKey` was accepted. Forget a parked copy of exactly
   * that text (the user left the thread while it was sending); a parked
   * draft that differs was written later and is kept.
   */
  settleSent(threadKey: string, sentText: string): void {
    if (threadKey && this.#drafts.get(threadKey) === sentText) this.#drafts.delete(threadKey)
  }

  peek(threadKey: string): string {
    return this.#drafts.get(threadKey) || ''
  }
}
