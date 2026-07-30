import { readFileSync } from 'node:fs'
import { defineConfig, loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue'

const applicationVersion = `v${readFileSync(
  new URL('../VERSION', import.meta.url),
  'utf8'
).trim()}`

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '')
  const apiProxyTarget = env.VITE_API_PROXY_TARGET || 'http://127.0.0.1:8080'

  return {
    plugins: [vue()],
    define: {
      'import.meta.env.VITE_MODEMDECK_BUILD_ID': JSON.stringify(applicationVersion)
    },
    build: {
      cssCodeSplit: false
    },
    server: {
      host: '0.0.0.0',
      port: 5173,
      strictPort: false,
      proxy: {
        '/api': {
          target: apiProxyTarget,
          changeOrigin: true,
          timeout: 120000,
          proxyTimeout: 120000
        }
      }
    }
  }
})
