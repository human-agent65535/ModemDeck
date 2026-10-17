import { computed, reactive, shallowRef } from 'vue'
import type { SendMessageInput } from '../api/types'

type Draft = {
  id: number
  threadKey: string
  composeKey: string | null
  text: string
  recipient: string
  lineID: string
  revision: number
  error: string
}

export type MessageComposerPresentation = Readonly<{
  draftID: number
  revision: number
  visit: number
}>

export type MessageSend = MessageComposerPresentation & Readonly<{
  input: Readonly<SendMessageInput>
}>

/**
 * One owner for each draft throughout this page's lifetime. Navigation changes
 * the active reference; only editing changes a draft's revision. A send owns a
 * snapshot, so settling it never depends on where its draft is displayed.
 */
export function useMessageComposer() {
  const drafts = new Map<number, Draft>()
  const threads = new Map<string, Draft>()
  const active = shallowRef<Draft | null>(null)
  // Preserve the page's single-send policy, even when another draft is opened.
  const pending = shallowRef<MessageSend | null>(null)
  let nextID = 0
  let visit = 0
  let disposed = false

  function create(threadKey: string, composeKey: string | null, recipient = ''): Draft {
    const draft = reactive<Draft>({
      id: ++nextID, threadKey, composeKey, recipient,
      text: '', lineID: '', revision: 0, error: ''
    })
    drafts.set(draft.id, draft)
    return draft
  }

  function activate(draft: Draft | null): void {
    if (active.value === draft) return
    const previous = active.value
    if (previous && previous.composeKey !== null) drafts.delete(previous.id)
    visit += 1
    active.value = draft
  }

  function openThread(key: string): void {
    if (disposed) return
    if (!key) { activate(null); return }
    let draft = threads.get(key)
    if (!draft) {
      draft = create(key, null)
      threads.set(key, draft)
    }
    activate(draft)
  }

  function openCompose(key: string, recipient = '', restart = false): boolean {
    if (disposed || (!restart && active.value?.composeKey === key)) return false
    activate(create('', key, recipient))
    return true
  }

  function close(): void { activate(null) }

  function forgetThread(key: string): void {
    const draft = threads.get(key)
    if (!draft) return
    if (active.value === draft) close()
    drafts.delete(draft.id)
    threads.delete(key)
  }

  function edit(field: 'text' | 'recipient' | 'lineID', value: string): void {
    const draft = active.value
    if (!draft || draft[field] === value) return
    draft[field] = value
    draft.revision += 1
    draft.error = ''
  }

  function field(name: 'text' | 'recipient' | 'lineID') {
    return computed({
      get: () => active.value?.[name] || '',
      set: (value: string) => edit(name, value)
    })
  }

  function presentation(): MessageComposerPresentation | null {
    const draft = active.value
    return draft ? { draftID: draft.id, revision: draft.revision, visit } : null
  }

  function isCurrent(snapshot: MessageComposerPresentation | null): boolean {
    return Boolean(snapshot && !disposed && active.value?.id === snapshot.draftID &&
      active.value.revision === snapshot.revision && visit === snapshot.visit)
  }

  function beginSend(target: Omit<SendMessageInput, 'content'>): MessageSend | null {
    const draft = active.value
    const snapshot = presentation()
    const content = draft?.text.trim()
    if (disposed || pending.value || !draft || !snapshot || !content) return null
    const send = Object.freeze({
      ...snapshot,
      input: Object.freeze({ ...target, content })
    })
    draft.error = ''
    pending.value = send
    return send
  }

  /** Clear the accepted version once; return whether its UI effects still belong here. */
  function succeed(send: MessageSend): boolean {
    if (pending.value !== send) return false
    const present = isCurrent(send)
    const draft = drafts.get(send.draftID)
    pending.value = null
    if (draft?.revision === send.revision) {
      draft.text = ''
      draft.error = ''
    }
    return present
  }

  function fail(send: MessageSend, message: string): void {
    if (pending.value !== send) return
    pending.value = null
    const draft = drafts.get(send.draftID)
    if (draft?.revision === send.revision) draft.error = message
  }

  function dispose(): void {
    close()
    disposed = true
    pending.value = null
    drafts.clear()
    threads.clear()
  }

  return {
    text: field('text'),
    recipient: field('recipient'),
    lineID: field('lineID'),
    threadKey: computed(() => active.value?.threadKey || ''),
    composingNew: computed(() => Boolean(active.value && active.value.composeKey !== null)),
    sending: computed(() => pending.value !== null),
    error: computed(() => active.value?.error || ''),
    openThread, openCompose, close, forgetThread,
    presentation, isCurrent, beginSend, succeed, fail, dispose
  }
}
