import { reactive } from 'vue'
import type { ModemDeckGateway } from '../api/gateway'
import type { CallTestStatus } from '../api/types'

type Dependencies = {
  gateway: Pick<ModemDeckGateway, 'startCallTest' | 'getCallTestStatus' | 'endCallTest'>
  startMedia: (id: string) => void
  stopMedia: (id: string) => void
  unavailable: () => boolean
  failureMessage: () => string
  notify: () => (() => void)
}

// Timers and late responses are scoped to a generation so a stopped test cannot
// attach a delayed microphone, including after logout or a real incoming call.
export function createCallTestController(dependencies: Dependencies) {
  const state = reactive<{
    status: 'idle' | 'scheduled' | 'ringing' | 'starting' | 'running' | 'ended' | 'error'
    session: CallTestStatus | null
    error: string
    connected: boolean
  }>({ status: 'idle', session: null, error: '', connected: false })
  let generation = 0
  let pollTimer: ReturnType<typeof setTimeout> | undefined
  let limitTimer: ReturnType<typeof setTimeout> | undefined
  let scheduleTimer: ReturnType<typeof setTimeout> | undefined
  let closeNotification: (() => void) | undefined
  let language = 'en-US'

  function clearTimers() {
    clearTimeout(pollTimer)
    clearTimeout(limitTimer)
    clearTimeout(scheduleTimer)
    pollTimer = limitTimer = scheduleTimer = undefined
    closeNotification?.()
    closeNotification = undefined
  }

  async function stop(error = ''): Promise<void> {
    if (!['scheduled', 'ringing', 'starting', 'running'].includes(state.status)) return
    const token = ++generation
    const id = state.session?.id
    clearTimers()
    if (id) dependencies.stopMedia(id)
    state.error = error
    state.status = error ? 'error' : 'ended'
    if (id) {
      try {
        await dependencies.gateway.endCallTest(id)
      } catch (cause) {
        if (token !== generation || state.error) return
        state.error = cause instanceof Error ? cause.message : dependencies.failureMessage()
        state.status = 'error'
      }
    }
  }

  async function poll(id: string, token: number): Promise<void> {
    try {
      const status = await dependencies.gateway.getCallTestStatus(id)
      if (token !== generation) return
      if (status.id !== id) throw new Error(dependencies.failureMessage())
      state.session = status
      if (status.phase !== 'active') {
        await stop(status.phase === 'failed' ? dependencies.failureMessage() : '')
        return
      }
      pollTimer = setTimeout(() => { void poll(id, token) }, 500)
    } catch (cause) {
      if (token !== generation) return
      await stop(cause instanceof Error ? cause.message : dependencies.failureMessage())
    }
  }

  function schedule(selectedLanguage: string): void {
    if (dependencies.unavailable() || ['scheduled', 'ringing', 'starting', 'running'].includes(state.status)) return
    const token = ++generation
    clearTimers()
    language = selectedLanguage
    state.session = null
    state.status = 'scheduled'
    state.error = ''
    state.connected = false
    scheduleTimer = setTimeout(() => {
      if (token !== generation || dependencies.unavailable()) { void stop(); return }
      state.status = 'ringing'
      closeNotification = dependencies.notify()
      limitTimer = setTimeout(() => { void stop() }, 30_000)
    }, 5000)
  }

  async function answer(): Promise<void> {
    if (state.status !== 'ringing') return
    if (dependencies.unavailable()) { await stop(); return }
    const token = ++generation
    clearTimers()
    state.status = 'starting'
    try {
      const status = await dependencies.gateway.startCallTest(language)
      if (token !== generation || dependencies.unavailable()) {
        await dependencies.gateway.endCallTest(status.id).catch(() => undefined)
        if (token === generation) state.status = 'ended'
        return
      }
      state.session = status
      if (status.phase !== 'active') throw new Error(dependencies.failureMessage())
      state.status = 'running'
      limitTimer = setTimeout(() => { void stop() }, 60_000)
      dependencies.startMedia(status.id)
      void poll(status.id, token)
    } catch (cause) {
      if (token !== generation) return
      await stop(cause instanceof Error ? cause.message : dependencies.failureMessage())
    }
  }

  return { state, schedule, answer, stop }
}
