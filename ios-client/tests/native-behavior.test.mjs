import assert from 'node:assert/strict'
import { execFile } from 'node:child_process'
import { mkdtemp, rm } from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'
import { promisify } from 'node:util'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const run = promisify(execFile)
const root = path.dirname(path.dirname(fileURLToPath(import.meta.url)))

test('media exchange retries preserve the offer, deadline and hangup cancellation', {
  skip: process.platform !== 'darwin', timeout: 60_000
}, async () => {
  const temporary = await mkdtemp(path.join(os.tmpdir(), 'modemdeck-media-request-test-'))
  try {
    const executable = path.join(temporary, 'media-request-tests')
    const env = { ...process.env, DEVELOPER_DIR: '/Applications/Xcode.app/Contents/Developer' }
    await run('xcrun', ['swiftc', '-swift-version', '5', '-parse-as-library',
      path.join(root, 'ios/App/App/ModemDeckDiagnostics.swift'),
      path.join(root, 'ios/App/App/ModemDeckMediaRequest.swift'),
      path.join(root, 'tests/media-request.swift'), '-o', executable], { env })
    const { stdout } = await run(executable, [], { env, timeout: 10_000 })
    assert.match(stdout, /7 media request behavior tests passed/)
  } finally { await rm(temporary, { recursive: true, force: true }) }
})

test('all native diagnostics honor consent, redaction, retries, persistence and pairing isolation', {
  skip: process.platform !== 'darwin', timeout: 60_000
}, async () => {
  const temporary = await mkdtemp(path.join(os.tmpdir(), 'modemdeck-diagnostics-test-'))
  try {
    const executable = path.join(temporary, 'diagnostics-tests')
    const env = { ...process.env, DEVELOPER_DIR: process.env.DEVELOPER_DIR || '/Applications/Xcode.app/Contents/Developer' }
    await run('xcrun', ['swiftc', '-swift-version', '5', '-parse-as-library',
      path.join(root, 'ios/App/App/ModemDeckDiagnostics.swift'),
      path.join(root, 'tests/diagnostics.swift'), '-o', executable
    ], { env })
    const { stdout } = await run(executable, [], { env, timeout: 15_000, maxBuffer: 2 * 1024 * 1024 })
    assert.match(stdout, /8 diagnostics behavior tests passed/)
  } finally { await rm(temporary, { recursive: true, force: true }) }
})

test('foreground recovery executes automatic retries, coalescing and cancellation', {
  skip: process.platform !== 'darwin', timeout: 60_000
}, async () => {
  const temporary = await mkdtemp(path.join(os.tmpdir(), 'modemdeck-recovery-test-'))
  try {
    const executable = path.join(temporary, 'recovery-tests')
    const env = { ...process.env, DEVELOPER_DIR: process.env.DEVELOPER_DIR || '/Applications/Xcode.app/Contents/Developer' }
    await run('xcrun', ['swiftc', '-swift-version', '6', '-parse-as-library',
      path.join(root, 'ios/App/App/ModemDeckConnectionRecovery.swift'),
      path.join(root, 'tests/connection-recovery.swift'), '-o', executable
    ], { env })
    const { stdout } = await run(executable, [], { env, timeout: 10_000 })
    assert.match(stdout, /6 connection recovery behavior tests passed/)
  } finally {
    await rm(temporary, { recursive: true, force: true })
  }
})

test('a path segment never lets an identifier reach another endpoint', {
  skip: process.platform !== 'darwin', timeout: 60_000
}, async () => {
  const { readFile, writeFile } = await import('node:fs/promises')
  const api = await readFile(path.join(root, 'ios/App/App/ModemDeckAPI.swift'), 'utf8')
  const start = api.indexOf('func modemDeckPathSegment(')
  const end = api.indexOf('\n}\n', start)
  assert.ok(start >= 0 && end > start, 'modemDeckPathSegment is missing')

  // Every identifier that goes into a request path uses the one function.
  for (const file of ['ModemDeckAPI.swift', 'ModemDeckCallAudio.swift', 'ModemDeckNative.swift',
    'ModemDeckSession.swift']) {
    const source = await readFile(path.join(root, 'ios/App/App', file), 'utf8')
    assert.doesNotMatch(source, /urlPathAllowed/, `${file} builds a path segment by hand`)
  }

  const temporary = await mkdtemp(path.join(os.tmpdir(), 'modemdeck-path-segment-test-'))
  try {
    const sourceFile = path.join(temporary, 'main.swift')
    await writeFile(sourceFile, `import Foundation
${api.slice(start, end + 3)}
let cases: [(String, String)] = [
  ("call_0123abcDEF-._~", "call_0123abcDEF-._~"),
  ("a/b", "a%2Fb"),
  ("../active", "..%2Factive"),
  ("..", "%2E%2E"),
  (".", "%2E"),
  ("id?x=1#y", "id%3Fx%3D1%23y"),
  ("a b%", "a%20b%25"),
  ("线路", "%E7%BA%BF%E8%B7%AF"),
  ("", "")
]
for (input, expected) in cases {
  precondition(modemDeckPathSegment(input) == expected, "\\(input) -> \\(modemDeckPathSegment(input))")
}
print("path segment cases passed")
`)
    const executable = path.join(temporary, 'path-segment-tests')
    const env = { ...process.env, DEVELOPER_DIR: process.env.DEVELOPER_DIR || '/Applications/Xcode.app/Contents/Developer' }
    await run('xcrun', ['swiftc', '-swift-version', '5', sourceFile, '-o', executable], { env })
    const { stdout } = await run(executable, [], { env, timeout: 10_000 })
    assert.match(stdout, /path segment cases passed/)
  } finally { await rm(temporary, { recursive: true, force: true }) }
})
