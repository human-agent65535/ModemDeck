import { readFileSync } from 'node:fs'
import { defineConfig, loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue'

function repositoryVersion(): string {
  return `v${readFileSync(new URL('../VERSION', import.meta.url), 'utf8').trim()}`
}

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '')
  const apiProxyTarget = env.VITE_API_PROXY_TARGET || 'http://127.0.0.1:8080'
  const applicationVersion =
    process.env.VITE_MODEMDECK_BUILD_ID?.trim() ||
    env.VITE_MODEMDECK_BUILD_ID?.trim() ||
    repositoryVersion()

  return {
    plugins: [
      vue(),
      {
        name: 'modemdeck-build-identity',
        generateBundle() {
          this.emitFile({
            type: 'asset',
            fileName: 'modemdeck-build.json',
            source: `${JSON.stringify({ version: applicationVersion })}\n`
          })
        }
      }
    ],
    define: {
      'import.meta.env.VITE_MODEMDECK_BUILD_ID': JSON.stringify(applicationVersion)
    },
    build: {
      cssCodeSplit: true,
      rolldownOptions: {
        output: {
          strictExecutionOrder: true,
          codeSplitting: {
            groups: [
              {
                name: 'vue-vendor',
                test: /node_modules[\\/](?:vue|@vue|vue-router|vue-i18n|@intlify)[\\/]/,
                priority: 30
              },
              {
                name: 'phone-vendor',
                test: /node_modules[\\/]libphonenumber-js[\\/]/,
                priority: 20
              },
              {
                name: 'icons',
                test: /node_modules[\\/]@lucide[\\/]vue[\\/]/,
                priority: 10
              },
              {
                name: 'app-shell',
                test: /web[\\/]src[\\/]/,
                tags: ['$initial'],
                priority: 5
              }
            ]
          }
        }
      }
    },
    server: {
      https: false,
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
