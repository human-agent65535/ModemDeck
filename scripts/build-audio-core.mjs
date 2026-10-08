import { createHash } from 'node:crypto'
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { spawnSync } from 'node:child_process'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const image = 'emscripten/emsdk:4.0.10@sha256:90b757eb11fa9a0e3ce4d2d9f76d932a56018e4accc37b5a28b2783751e60eb7'
const sourcePaths = ['internal/audiocore/audio_core.h', 'internal/audiocore/audio_core.c',
  'scripts/audio-core/wasm.c', 'scripts/build-audio-core.mjs']
const exports = ['md_clock_reset', 'md_clock_accept', 'md_clock_play_at', 'md_clock_age', 'md_clock_generation', 'md_clock_source_samples',
  'md_audio_frame_samples', 'md_audio_queue_capacity', 'md_audio_send_queue_capacity',
  'md_audio_frame_seconds', 'md_audio_prebuffer_seconds', 'md_audio_max_age_seconds',
  'md_audio_frame_expired', 'md_audio_playback_start', 'md_audio_source_slot']
const wasmPath = path.join(root, 'web/src/state/audioCore.wasm')
const manifestPath = path.join(root, 'web/src/state/audioCore.build.json')
const fingerprint = createHash('sha256')
for (const sourcePath of sourcePaths) {
  fingerprint.update(sourcePath).update('\0').update(await readFile(path.join(root, sourcePath))).update('\0')
}
const sourceSHA = fingerprint.digest('hex')
const digest = bytes => createHash('sha256').update(bytes).digest('hex')
function verify(bytes) {
  const module = new WebAssembly.Module(bytes)
  if (WebAssembly.Module.imports(module).length) throw new Error('Audio core must have no runtime imports')
  const instance = new WebAssembly.Instance(module)
  if (instance.exports.memory.buffer.byteLength !== 65536) throw new Error('Audio core memory must stay bounded at 64 KiB')
  for (const name of exports) {
    if (typeof instance.exports[name] !== 'function') throw new Error(`Missing audio core export: ${name}`)
  }
  return instance
}
if (process.argv.includes('--check')) {
  const manifest = JSON.parse(await readFile(manifestPath, 'utf8'))
  const bytes = await readFile(wasmPath)
  if (manifest.source_sha256 !== sourceSHA || manifest.wasm_sha256 !== digest(bytes) || manifest.compiler !== image) {
    throw new Error('Shared audio source/artifact changed; run make audio-core, then commit both outputs')
  }
  verify(bytes)
  console.log(`Shared audio core verified (${bytes.length} bytes, source ${sourceSHA.slice(0, 12)})`)
} else {
  const output = await mkdtemp(path.join(tmpdir(), 'modemdeck-audio-core-'))
  try {
    const args = ['run', '--rm', '--platform', 'linux/amd64',
      '--mount', `type=bind,source=${root},target=/workspace,readonly`,
      '--mount', `type=bind,source=${output},target=/out`, '-w', '/workspace', image,
      'emcc', 'internal/audiocore/audio_core.c', 'scripts/audio-core/wasm.c', '-Iinternal/audiocore',
      '-std=c11', '-Oz', '-Wall', '-Wextra', '-Werror', '--no-entry',
      '-sSTANDALONE_WASM=1', '-sFILESYSTEM=0', '-sASSERTIONS=0',
      '-sSTACK_SIZE=16384', '-sINITIAL_MEMORY=65536', '-sALLOW_MEMORY_GROWTH=0',
      `-sEXPORTED_FUNCTIONS=${JSON.stringify(exports.map(name => '_' + name))}`,
      '-o', '/out/audioCore.wasm']
    const result = spawnSync('docker', args, { stdio: 'inherit' })
    if (result.error) throw result.error
    if (result.status !== 0) throw new Error(`Audio core compilation exited ${result.status}`)
    const bytes = await readFile(path.join(output, 'audioCore.wasm'))
    verify(bytes)
    await writeFile(wasmPath, bytes)
    await writeFile(manifestPath, JSON.stringify({ schema: 1, compiler: image,
      source_sha256: sourceSHA, wasm_sha256: digest(bytes) }, null, 2) + '\n')
    console.log(`Built shared audio core (${bytes.length} bytes)`)
  } finally {
    await rm(output, { recursive: true, force: true })
  }
}
