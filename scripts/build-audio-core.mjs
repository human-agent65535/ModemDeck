import { mkdir, readFile, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'
import { spawnSync } from 'node:child_process'
import { compilerImage, digest, sourceFingerprint } from './audio-core/sources.mjs'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const outputDirectory = path.join(root, 'web/src/state')
const manifestPath = path.join(outputDirectory, 'audioCore.build.json')
const sourceSHA = await sourceFingerprint(root)
async function verify() {
  const manifest = JSON.parse(await readFile(manifestPath, 'utf8'))
  const wasm = await readFile(path.join(outputDirectory, 'audioCore.wasm'))
  const glue = await readFile(path.join(outputDirectory, 'audioCore.mjs'))
  if (manifest.source_sha256 !== sourceSHA || manifest.wasm_sha256 !== digest(wasm)
    || manifest.glue_sha256 !== digest(glue) || manifest.compiler !== compilerImage) {
    throw new Error('Audio sources/artifacts changed; run make audio-core and commit all three outputs')
  }
  const { default: createAudioCore } = await import(pathToFileURL(path.join(outputDirectory, 'audioCore.mjs')))
  const core = createAudioCore({ wasmBinary: wasm })
  if (core && typeof core.then === 'function') throw new Error('AudioWorklet requires synchronous initialized Wasm')
  if (core.HEAPU8.buffer.byteLength !== 33554432) throw new Error('Unexpected audio memory budget')
  if (typeof SharedArrayBuffer !== 'undefined' && core.HEAPU8.buffer instanceof SharedArrayBuffer) throw new Error('Audio core must work without cross-origin isolation')
  const receiver = core._md_neteq_create(16000)
  if (!receiver) throw new Error('Could not initialize the production NetEq receiver')
  core._md_neteq_destroy(receiver)
  console.log(`Shared NetEq/Opus verified (${wasm.length} Wasm bytes; source ${sourceSHA.slice(0, 12)})`)
}
if (!process.argv.includes('--check')) {
  const output = path.join(root, 'dist/audio-core/wasm')
  await mkdir(output, { recursive: true })
  {
    const result = spawnSync('docker', ['run', '--rm', '--platform', 'linux/amd64',
      '--mount', `type=bind,source=${root},target=/workspace,readonly`,
      '--mount', `type=bind,source=${output},target=/out`, '-w', '/workspace', compilerImage,
      'sh', '-ec', 'emcmake cmake -S internal/audiocore/native -B /out/build && cmake --build /out/build --target md_audio_module --parallel 4'], { stdio: 'inherit' })
    if (result.error) throw result.error
    if (result.status !== 0) throw new Error(`Audio core build exited ${result.status}`)
    if (await sourceFingerprint(root) !== sourceSHA) throw new Error('Audio sources changed during compilation; rebuild before publishing artifacts')
    const wasm = await readFile(path.join(output, 'build/audioCore.wasm'))
    const glue = await readFile(path.join(output, 'build/audioCore.mjs'))
    await writeFile(path.join(outputDirectory, 'audioCore.wasm'), wasm)
    await writeFile(path.join(outputDirectory, 'audioCore.mjs'), glue)
    await writeFile(manifestPath, JSON.stringify({ schema: 2, compiler: compilerImage, source_sha256: sourceSHA,
      wasm_sha256: digest(wasm), glue_sha256: digest(glue), memory_bytes: 33554432, threads: false }, null, 2) + '\n')
  }
}
await verify()
