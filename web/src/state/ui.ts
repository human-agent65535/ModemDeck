import { reactive } from 'vue'

export const uiState = reactive({
  dialerOpen: false,
  dialTarget: '',
  dialLabel: '',
  dialRequestRevision: 0
})

export function openDialer(target = '', label = ''): void {
  uiState.dialTarget = target
  uiState.dialLabel = label
  uiState.dialRequestRevision += 1
  uiState.dialerOpen = true
}

export function closeDialer(): void {
  uiState.dialerOpen = false
}
