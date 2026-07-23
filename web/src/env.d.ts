/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_MODEMDECK_FIXTURE?: string
  readonly VITE_API_PROXY_TARGET?: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}

declare module '*.vue' {
  import type { DefineComponent } from 'vue'

  const component: DefineComponent<object, object, unknown>
  export default component
}
