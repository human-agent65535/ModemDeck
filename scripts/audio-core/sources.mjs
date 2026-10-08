import { createHash } from 'node:crypto'
import { readFile, readdir } from 'node:fs/promises'
import path from 'node:path'
export const compilerImage = 'emscripten/emsdk:4.0.10@sha256:90b757eb11fa9a0e3ce4d2d9f76d932a56018e4accc37b5a28b2783751e60eb7'
export const digest = bytes => createHash('sha256').update(bytes).digest('hex')
export async function sourceFingerprint(root) {
  const paths = ['third_party/audio/upstream.json', 'scripts/build-audio-core.mjs', 'scripts/audio-core/sources.mjs']
  async function add(directory) {
    for (const entry of await readdir(path.join(root, directory), { withFileTypes: true })) {
      const name = `${directory}/${entry.name}`
      if (entry.isDirectory()) await add(name)
      else if (entry.isFile()) paths.push(name)
    }
  }
  await add('internal/audiocore/native')
  const hash = createHash('sha256')
  for (const name of paths.sort()) hash.update(name).update('\0').update(await readFile(path.join(root, name))).update('\0')
  const vendor = JSON.parse(await readFile(path.join(root, 'third_party/audio/upstream.json'), 'utf8'))
  const archive = await readFile(path.join(root, 'third_party/audio', vendor.archive))
  if (digest(archive) !== vendor.archive_sha256) throw new Error('Vendored audio source checksum mismatch')
  return hash.digest('hex')
}
