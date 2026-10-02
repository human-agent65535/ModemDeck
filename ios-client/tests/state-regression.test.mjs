import assert from 'node:assert/strict'
import { readFile, mkdtemp, writeFile, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { spawnSync } from 'node:child_process'
import test from 'node:test'

function declaration(source, marker) {
  const start = source.indexOf(marker)
  assert.ok(start >= 0, `missing ${marker}`)
  const body = source.indexOf('{', start)
  let depth = 0
  for (let index = body; index < source.length; index++) {
    if (source[index] === '{') depth++
    if (source[index] === '}' && --depth === 0) return source.slice(start, index + 1)
  }
  assert.fail(`unclosed ${marker}`)
}

async function verify(template, declarations) {
  const directory = await mkdtemp(path.join(tmpdir(), 'modemdeck-swift-regression-'))
  try {
    const stubs = await readFile(new URL(`fixtures/${template}`, import.meta.url), 'utf8')
    const file = path.join(directory, 'test.swift')
    const binary = path.join(directory, 'test')
    await writeFile(file, stubs.replace('// INSERT_PRODUCT_METHODS', declarations.join('\n')))
    const compiled = spawnSync('xcrun', ['swiftc', '-parse-as-library', file,
      new URL('fixtures/diagnostics-stub.swift', import.meta.url).pathname, '-o', binary], {
      env: { ...process.env, DEVELOPER_DIR: process.env.DEVELOPER_DIR || '/Applications/Xcode.app/Contents/Developer' }, encoding: 'utf8'
    })
    assert.equal(compiled.status, 0, compiled.stderr)
    const result = spawnSync(binary, [], { encoding: 'utf8' })
    assert.equal(result.status, 0, result.stderr)
  } finally { await rm(directory, { recursive: true, force: true }) }
}

test('native collections fetch all pages, retain filters, and reject partial snapshots', { skip: process.platform !== 'darwin' }, async () => {
  const source = await readFile(new URL('../ios/App/App/ModemDeckAPI.swift', import.meta.url), 'utf8')
  await verify('pagination-stubs.swift', ['func contacts(', 'func calls()', 'func recordings()',
    'private func allPages<', 'private func path('].map(marker => declaration(source, marker)))
})

test('call stream decodes server fields, ownership-only updates and rejects malformed events', { skip: process.platform !== 'darwin' }, async () => {
  const source = await readFile(new URL('../ios/App/App/ModemDeckNative.swift', import.meta.url), 'utf8')
  await verify('call-stream-stubs.swift', [
    'private struct ModemDeckRuntimeCallState:',
    'private final class ModemDeckRuntimeCallStream:'
  ].map(marker => declaration(source, marker)))
})

test('audio teardown survives owner release and mute survives track creation', { skip: process.platform !== 'darwin' }, async () => {
  const source = await readFile(new URL('../ios/App/App/ModemDeckCallAudio.swift', import.meta.url), 'utf8')
  await verify('call-audio-lifetime-stubs.swift', [
    'func setMuted(_ muted: Bool)', 'func stop()', 'private func installLocalAudioTrack('
  ].map(marker => declaration(source, marker).replace('private func', 'func')))
})

test('native conversation refresh removes deleted cache entries and send completion preserves newer drafts', { skip: process.platform !== 'darwin' }, async () => {
  const source = await readFile(new URL('../ios/App/App/ModemDeckSession.swift', import.meta.url), 'utf8')
  await verify('conversation-stubs.swift', ['final class ModemDeckConversationStore:', 'final class ModemDeckMessageDraft:']
    .map(marker => '@MainActor\n' + declaration(source, marker)))
})

test('activity snapshot preserves all historical rows, date ordering and stable identities', { skip: process.platform !== 'darwin' }, async () => {
  const source = await readFile(new URL('../ios/App/App/ModemDeckHomeView.swift', import.meta.url), 'utf8')
  await verify('activity-stubs.swift', ['private enum ModemDeckActivityItem:',
    'private struct ModemDeckActivitySource:', 'private struct ModemDeckActivityDay:',
    'private struct ModemDeckActivitySnapshot'].map(marker => declaration(source, marker)))
})


test('conversation read receipts cover historical imports without acknowledging later arrivals', { skip: process.platform !== 'darwin' }, async () => {
  const source = await readFile(new URL('../ios/App/App/ModemDeckCommunicationViews.swift', import.meta.url), 'utf8')
  await verify('message-read-stubs.swift', ['private var readThroughMessageID:', 'private var newestIncomingMessageID:']
    .map(marker => declaration(source, marker).replace('private var', 'var')))
})

test('CallKit end closes locally without an HTTP response and auxiliary timeouts preserve calls', { skip: process.platform !== 'darwin' }, async () => {
  const source = await readFile(new URL('../ios/App/App/ModemDeckNative.swift', import.meta.url), 'utf8')
  await verify('callkit-stubs.swift', [
    'func provider(_ provider: CXProvider, perform action: CXEndCallAction)',
    'func provider(_ provider: CXProvider, timedOutPerforming action: CXAction)',
    'private func endCallVerb(', 'private func isLocalTestCall(',
    'private func reportCallEnded('
  ].map(marker => declaration(source, marker)))
})

test('call termination survives answer races, ambiguous responses and background suspension', { skip: process.platform !== 'darwin' }, async () => {
  const source = await readFile(new URL('../ios/App/App/ModemDeckNative.swift', import.meta.url), 'utf8')
  await verify('call-end-operation-stubs.swift', [declaration(source, 'private final class ModemDeckCallEndOperation')])
})

test('in-app hangup interrupts answering and ignores errors for an ended call', { skip: process.platform !== 'darwin' }, async () => {
  const source = await readFile(new URL('../ios/App/App/ModemDeckSession.swift', import.meta.url), 'utf8')
  await verify('call-controller-stubs.swift', ['func end() async', 'private func perform(']
    .map(marker => declaration(source, marker)))
})

test('outgoing media completions cannot revive a removed call', { skip: process.platform !== 'darwin' }, async () => {
  const source = await readFile(new URL('../ios/App/App/ModemDeckNative.swift', import.meta.url), 'utf8')
  await verify('call-outgoing-stubs.swift', [declaration(source, 'audioSession.connect { [weak self, weak audioSession] result in')])
})

test('runtime reconciliation accepts ownership-only changes and rejects obsolete streams', { skip: process.platform !== 'darwin' }, async () => {
  const source = await readFile(new URL('../ios/App/App/ModemDeckNative.swift', import.meta.url), 'utf8')
  await verify('call-state-cursor-stubs.swift', [declaration(source, 'private func acceptRuntimeCallState(').replace('private func', 'func')])
})

test('real and test call ownership heartbeats renew before a WebRTC track exists', { skip: process.platform !== 'darwin' }, async () => {
  const source = await readFile(new URL('../ios/App/App/ModemDeckCallAudio.swift', import.meta.url), 'utf8')
  await verify('call-heartbeat-stubs.swift', ['func startControlHeartbeat()', 'private func startLeaseHeartbeat()', 'private func callPath(']
    .map(marker => declaration(source, marker)))
})

test('persisted hangups recover only with the original credential and reconcile before sending', { skip: process.platform !== 'darwin' }, async () => {
  const source = await readFile(new URL('../ios/App/App/ModemDeckNative.swift', import.meta.url), 'utf8')
  const declarations = ['struct ModemDeckCredential:', 'private struct ModemDeckCallEndIntent:']
    .map(marker => declaration(source, marker))
  // Types live outside the coordinator; the fixture inserts the actual recovery method separately.
  const recovery = declaration(source, 'private func resumePendingCallEnds()').replace('private func', 'func')
  await verify('call-end-recovery-stubs.swift', [declarations.join('\n') + '\n' + recoveryFixture(recovery)])
})

function recoveryFixture(method) {
  return `private final class Recovery {\n${method}\n` + `
    var credential: ModemDeckCredential?
    var pendingEndIntents: [UUID: ModemDeckCallEndIntent] = [:]
    var pendingCallEnds: [UUID: Operation] = [:]
    var installed: [UUID] = [], remembered: [UUID] = []
    var backgrounds = 0
    func loadCredential() -> ModemDeckCredential? { credential }
    func rememberEndedCall(_ uuid: UUID) { remembered.append(uuid) }
    func beginCallEndBackgroundTask(_ uuid: UUID) { backgrounds += 1 }
    func finishCallEndBackgroundTask(_ uuid: UUID) {}
    func installPendingCallEnd(_ intent: ModemDeckCallEndIntent, credential: ModemDeckCredential, reconcileFirst: Bool) {
        precondition(reconcileFirst)
        installed.append(intent.uuid)
        pendingCallEnds[intent.uuid] = Operation()
    }
}`
}

test('CallKit repeated answers share permission and connection outcomes without reconnecting active calls', { skip: process.platform !== 'darwin' }, async () => {
  const source = await readFile(new URL('../ios/App/App/ModemDeckNative.swift', import.meta.url), 'utf8')
  await verify('callkit-answer-stubs.swift', [
    'func provider(_ provider: CXProvider, perform action: CXAnswerCallAction)',
    'private func finishProviderCallAction(', 'private func isLocalTestCall('
  ].map(marker => declaration(source, marker)))
})
