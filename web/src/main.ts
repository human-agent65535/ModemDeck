import { createApp } from 'vue'
import App from './App.vue'
import { i18n } from './i18n'
import router from './router'
import { ensureSession } from './state/session'
import './style.css'

async function mount(): Promise<void> {
  await ensureSession()
  createApp(App).use(i18n).use(router).mount('#app')
}

void mount()
