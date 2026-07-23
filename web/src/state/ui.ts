import { reactive } from 'vue'

export const uiState = reactive({
  dialerOpen: false,
  dialTarget: '',
  dialLabel: '',
  dialLineKey: '',
  dialRequestRevision: 0
})

export function openDialer(target = '', label = '', lineKey = ''): void {
  uiState.dialTarget = target
  uiState.dialLabel = label
  uiState.dialLineKey = lineKey
  uiState.dialRequestRevision += 1
  uiState.dialerOpen = true
}

export function closeDialer(): void {
  uiState.dialerOpen = false
}
