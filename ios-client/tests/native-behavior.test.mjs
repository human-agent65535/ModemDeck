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
