import type { AudioCoreModule } from './audioCore.ts'
declare function createAudioCore(options: { wasmBinary: ArrayBuffer; locateFile?: (name: string) => string; print?: (text: string) => void; printErr?: (text: string) => void }): AudioCoreModule
export default createAudioCore
