import { reactive } from 'vue'

export const uiState = reactive({
  dialerOpen: false,
  callMinimized: false,
  dialTarget: '',
  dialLabel: '',
  dialLineKey: '',
  dialImmediately: false,
  dialRequestRevision: 0
})

function requestDialer(
  target: string,
  label: string,
  lineKey: string,
  dialImmediately: boolean
): void {
  uiState.callMinimized = false
  uiState.dialTarget = target
  uiState.dialLabel = label
  uiState.dialLineKey = lineKey
  uiState.dialImmediately = dialImmediately
  uiState.dialRequestRevision += 1
  uiState.dialerOpen = true
}

export function openDialer(target = '', label = '', lineKey = ''): void {
  requestDialer(target, label, lineKey, false)
}

export function openDialerAndCall(target: string, label = '', lineKey = ''): void {
  requestDialer(target, label, lineKey, true)
}

export function closeDialer(): void {
  uiState.dialerOpen = false
}

export function showCallSurface(): void {
  uiState.callMinimized = false
}

export function minimizeCallSurface(): void {
  uiState.callMinimized = true
  uiState.dialerOpen = false
}
