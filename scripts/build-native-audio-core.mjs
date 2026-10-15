import { cp, mkdir, readFile, rm, writeFile, access } from 'node:fs/promises'
import { constants } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { spawnSync } from 'node:child_process'
import { digest, sourceFingerprint } from './audio-core/sources.mjs'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const native = path.join(root, 'internal/audiocore/native')
const base = path.join(root, 'dist/audio-core')
const developer = process.env.DEVELOPER_DIR || '/Applications/Xcode.app/Contents/Developer'
const env = { ...process.env, DEVELOPER_DIR: developer }
function run(command, args) {
  const result = spawnSync(command, args, { env, stdio: 'inherit' })
  if (result.error) throw result.error
  if (result.status !== 0) throw new Error(`${command} exited ${result.status}`)
}
async function exists(name) { try { await access(name, constants.R_OK); return true } catch { return false } }
const mode = process.argv.includes('--host') ? 'host' : 'ios'
const source = await sourceFingerprint(root)
const xcode = spawnSync('xcodebuild', ['-version'], { env, encoding: 'utf8' })
if (xcode.status !== 0) throw new Error('Xcode is required to build the native audio core')
const buildKey = digest(source + xcode.stdout + await readFile(fileURLToPath(import.meta.url)))
const output = path.join(base, mode)
const marker = path.join(output, 'build.sha256')
const artifact = path.join(output, mode === 'ios' ? 'MDNetEq.xcframework' : 'libmd_audio_core.a')
if (await exists(marker) && (await readFile(marker, 'utf8')).trim() === buildKey && await exists(artifact)) {
  console.log(`Native audio core is current (${mode}, ${source.slice(0, 12)})`)
  process.exit(0)
}
await mkdir(output, { recursive: true })
const headers = path.join(output, 'include')
await mkdir(headers, { recursive: true })
await cp(path.join(native, 'include/md_neteq.h'), path.join(headers, 'md_neteq.h'))
await writeFile(path.join(headers, 'module.modulemap'), 'module ModemDeckAudioCore {\n  header "md_neteq.h"\n  export *\n}\n')
await cp(path.join(root, 'third_party/audio/LICENSES.txt'), path.join(output, 'AudioCoreNotices.txt'))
const configurations = mode === 'host' ? [['host', []]] : [
  ['device', ['-DCMAKE_SYSTEM_NAME=iOS', '-DCMAKE_OSX_SYSROOT=iphoneos', '-DCMAKE_OSX_ARCHITECTURES=arm64', '-DCMAKE_SYSTEM_PROCESSOR=arm64', '-DCMAKE_OSX_DEPLOYMENT_TARGET=16.0']],
  ['simulator', ['-DCMAKE_SYSTEM_NAME=iOS', '-DCMAKE_OSX_SYSROOT=iphonesimulator', '-DCMAKE_OSX_ARCHITECTURES=arm64;x86_64', '-DCMAKE_OSX_DEPLOYMENT_TARGET=16.0']],
]
const libraries = []
for (const [name, args] of configurations) {
  const build = path.join(base, `build-${name}`)
  run('cmake', ['-S', native, '-B', build, ...args])
  run('cmake', ['--build', build, '--target', 'md_audio_core_bundle', '--parallel', '4'])
  libraries.push(path.join(build, 'libmd_audio_core.a'))
}
if (mode === 'host') await cp(libraries[0], artifact)
else {
  await rm(artifact, { recursive: true, force: true })
  run('xcodebuild', ['-create-xcframework', ...libraries.flatMap(library => ['-library', library, '-headers', headers]), '-output', artifact])
}
if (await sourceFingerprint(root) !== source) throw new Error('Audio sources changed during compilation; rerun the native build')
await writeFile(marker, buildKey + '\n')
console.log(`Built native NetEq/Opus (${mode}, ${source.slice(0, 12)})`)
