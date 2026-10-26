import { createApp } from 'vue'
import App from './App.vue'
import { i18n } from './i18n'
import { ensureSession } from './state/session'
import { initializeTheme } from './state/theme'
import {
  checkForApplicationUpdate,
  installStaleAssetRecovery
} from './state/staleAssetRecovery'
import './style.css'

initializeTheme()
if (!import.meta.env.DEV) installStaleAssetRecovery()

async function mount(): Promise<void> {
  if (!import.meta.env.DEV) await checkForApplicationUpdate()
  const { default: router } = await import('./router')
  await ensureSession()
  createApp(App).use(i18n).use(router).mount('#app')
}

void mount()
