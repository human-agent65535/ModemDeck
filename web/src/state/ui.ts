import { reactive } from 'vue'

export const uiState = reactive({
  dialerOpen: false,
  dialTarget: '',
  dialLabel: ''
})

export function openDialer(target = '', label = ''): void {
  uiState.dialTarget = target
  uiState.dialLabel = label
  uiState.dialerOpen = true
}

export function closeDialer(): void {
  uiState.dialerOpen = false
}
