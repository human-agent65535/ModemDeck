import { reactive } from 'vue'

export const uiState = reactive({
  dialerOpen: false,
  callMinimized: false,
  dialTarget: '',
  dialLabel: '',
  dialLineKey: '',
  dialRequestRevision: 0
})

export function openDialer(target = '', label = '', lineKey = ''): void {
  uiState.callMinimized = false
  uiState.dialTarget = target
  uiState.dialLabel = label
  uiState.dialLineKey = lineKey
  uiState.dialRequestRevision += 1
  uiState.dialerOpen = true
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
