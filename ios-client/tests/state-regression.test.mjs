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
    const needsCore = declarations.some(value => /md_(?:audio|neteq|opus)_/.test(value))
    const core = new URL('../../dist/audio-core/host/', import.meta.url).pathname
    const coreArguments = needsCore ? ['-I', path.join(core, 'include'), path.join(core, 'libmd_audio_core.a'),
      '-lc++', '-framework', 'CoreFoundation', '-Xlinker', '-dead_strip'] : []
    await writeFile(file, (needsCore ? 'import ModemDeckAudioCore\n' : '') + stubs.replace('// INSERT_PRODUCT_GLOBALS', declarations.filter(value => /^(?:enum ModemDeckCallAudioError|struct ModemDeckAudioPacket|private final class ModemDeckAudioRenderer)/.test(value)).join('\n')).replace('// INSERT_PRODUCT_METHODS', declarations.filter(value => !stubs.includes('// INSERT_PRODUCT_GLOBALS') || !/^(?:enum ModemDeckCallAudioError|struct ModemDeckAudioPacket|private final class ModemDeckAudioRenderer)/.test(value)).join('\n')))
    const compiled = spawnSync('xcrun', ['swiftc', '-parse-as-library', file,
      new URL('fixtures/diagnostics-stub.swift', import.meta.url).pathname, ...coreArguments, '-o', binary], {
      env: { ...process.env, DEVELOPER_DIR: process.env.DEVELOPER_DIR || '/Applications/Xcode.app/Contents/Developer' }, encoding: 'utf8'
    })
    assert.equal(compiled.status, 0, compiled.stderr)
    const result = spawnSync(binary, [], { encoding: 'utf8' })
    assert.equal(result.status, 0, result.stderr)
    return result.stdout
  } finally { await rm(directory, { recursive: true, force: true }) }
}

test('native message and search queries preserve phone prefixes and reserved characters on the server', { skip: process.platform !== 'darwin' }, async () => {
  const source = await readFile(new URL('../ios/App/App/ModemDeckAPI.swift', import.meta.url), 'utf8')
  const output = await verify('query-encoding-stubs.swift', [
    'func messagePage(', 'private func listPath(', 'private func path('
  ].map(marker => declaration(source, marker)))
  const cases = JSON.parse(output)
  assert.equal(cases.length, 13)
  for (const { path: requestPath, parameters } of cases) {
    // Like Go's net/url, URLSearchParams interprets a literal query '+' as a space.
    const url = new URL(requestPath, 'https://example.invalid')
    assert.equal(url.pathname, 'peer' in parameters ? '/api/v1/messages' : '/api/v1/messages/threads')
    assert.equal(url.hash, '')
    assert.deepEqual(Object.fromEntries(url.searchParams), parameters, requestPath)
  }
})

test('native collections fetch all pages, retain filters, and reject partial snapshots', { skip: process.platform !== 'darwin' }, async () => {
  const source = await readFile(new URL('../ios/App/App/ModemDeckAPI.swift', import.meta.url), 'utf8')
  await verify('pagination-stubs.swift', ['func contacts(', 'func calls()', 'func recordings()',
    'private func allPages<', 'private func isCurrentCredential(', 'private func path('].map(marker => declaration(source, marker)))
})

test('call stream decodes server fields, ownership-only updates and rejects malformed events', { skip: process.platform !== 'darwin' }, async () => {
  const source = await readFile(new URL('../ios/App/App/ModemDeckNative.swift', import.meta.url), 'utf8')
  await verify('call-stream-stubs.swift', [
    'struct ModemDeckRuntimeCallState:',
    'final class ModemDeckRuntimeCallStream:'
  ].map(marker => declaration(source, marker)))
})

test('audio teardown survives owner release and mute persists before capture starts', { skip: process.platform !== 'darwin' }, async () => {
  const source = await readFile(new URL('../ios/App/App/ModemDeckCallAudio.swift', import.meta.url), 'utf8')
  await verify('call-audio-lifetime-stubs.swift', [
    'func setMuted(_ muted: Bool)', 'func stop()'
  ].map(marker => declaration(source, marker).replace('private func', 'func')))
})

test('native voice graph fixes both directions to mono, preserves the engine across notifications and reports failures', { skip: process.platform !== 'darwin' }, async () => {
  const source = await readFile(new URL('../ios/App/App/ModemDeckCallAudio.swift', import.meta.url), 'utf8')
  await verify('voice-graph-stubs.swift', [
    'enum ModemDeckCallAudioError:', 'private enum ModemDeckAudioDropReason', 'private struct ModemDeckAudioDropCounts',
    'private func recordDroppedFrames(', 'private func resetCaptureStream(', 'private func updateCaptureDropCounts()', 'private func captureBatch(', 'private func startAudioIfReady()',
    'private func matchesVoiceFormat(', 'private func voiceGraphMatches(',
    'private func configureVoiceGraph(', 'private func startVoiceGraph(',
    'private func audioConfigurationChanged(',
    'private func stopAudio()', 'private func failMedia(', 'private func finishConnection('
  ].map(marker => declaration(source, marker)))
})

test('native NetEq adapter uses real Opus, demand pulls, wrap metadata and bounded transport backpressure', { skip: process.platform !== 'darwin' }, async () => {
  const source = await readFile(new URL('../ios/App/App/ModemDeckCallAudio.swift', import.meta.url), 'utf8')
  await verify('neteq-native-stubs.swift', ['enum ModemDeckCallAudioError:', 'struct ModemDeckAudioPacket {',
    'private final class ModemDeckAudioRenderer {', 'private enum ModemDeckAudioDropReason',
    'private struct ModemDeckAudioDropCounts', 'private func recordDroppedFrames(', 'private func capture(',
    'private func sendNext(', 'private func checkSendDeadline(', 'private func receiveAudio(', 'private func resetMediaForReady()',
    'private func updateRenderedAudioStatistics()', 'private func neteqStatisticsFields('
  ].map(marker => declaration(source, marker)))
})

test('native drop accounting and server stats expose only enumerated numeric facts', { skip: process.platform !== 'darwin' }, async () => {
  const source = await readFile(new URL('../ios/App/App/ModemDeckCallAudio.swift', import.meta.url), 'utf8')
  const route = await readFile(new URL('../ios/App/App/ModemDeckAudioRoute.swift', import.meta.url), 'utf8')
  await verify('audio-observability-stubs.swift', ['private enum ModemDeckAudioDropReason',
    'private struct ModemDeckAudioDropCounts', 'private func recordDroppedFrames(',
    'private func updateServerAudioStatistics(', 'private func localAudioStatisticsFields()', 'private func recordConnectionEvent('
  ].map(marker => declaration(source, marker)).concat('final class ModemDeckAudioRoute {\n' + ['static func fields(', 'private static func port('].map(marker => declaration(route, marker)).join('\n') + '\n}'))
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

test('real and test call ownership heartbeats renew before capture exists', { skip: process.platform !== 'darwin' }, async () => {
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


test('WSS audio framing validates header, limits, byte order and wrapping counters', { skip: process.platform !== 'darwin' }, async () => {
  const source = await readFile(new URL('../ios/App/App/ModemDeckCallAudio.swift', import.meta.url), 'utf8')
  await verify('audio-packet-stubs.swift', [declaration(source, 'struct ModemDeckAudioPacket {')])
})


test('transient server media errors recover transport without ending the call', { skip: process.platform !== 'darwin' }, async () => {
  const source = await readFile(new URL('../ios/App/App/ModemDeckCallAudio.swift', import.meta.url), 'utf8')
  await verify('media-error-stubs.swift', [declaration(source, 'private func handleServerError(')])
})

test('WSS reconnect waits for successful owner cleanup and stops at its deadline', { skip: process.platform !== 'darwin' }, async () => {
  const source = await readFile(new URL('../ios/App/App/ModemDeckCallAudio.swift', import.meta.url), 'utf8')
  await verify('media-release-stubs.swift', [declaration(source, 'private func releaseForReconnect(')])
})


test('repeated early ready/errors preserve the retry budget until duplex media stabilizes', { skip: process.platform !== 'darwin' }, async () => {
  const source = await readFile(new URL('../ios/App/App/ModemDeckCallAudio.swift', import.meta.url), 'utf8')
  await verify('media-health-stubs.swift', ['struct ModemDeckMediaRecoveryHealth {',
    'private func markSocketReady(', 'private func clearRecoveryAfterStableMedia('].map(marker => declaration(source, marker)))
})


test('transport failure accounts queued media and exhausted recovery reports failure rather than remote hangup', { skip: process.platform !== 'darwin' }, async () => {
  const source = await readFile(new URL('../ios/App/App/ModemDeckCallAudio.swift', import.meta.url), 'utf8')
  await verify('media-transport-stubs.swift', [declaration(source, 'private func transportFailed(')])
})


test('durable SSE revisions refresh history after call end and recording finalization without losing in-flight invalidations', { skip: process.platform !== 'darwin' }, async () => {
  const source = await readFile(new URL('../ios/App/App/ModemDeckSession.swift', import.meta.url), 'utf8')
  const methods = ['private func startRuntimeEvents()', 'private func scheduleRuntimeEventsReconnect()',
    'private func stopRuntimeEvents(', 'private func acceptRuntimeHeartbeat(', 'private func acceptRuntimeEvent(',
    'private func requestRuntimeCollectionsRefresh()', 'func refreshCollections()']
    .map(marker => declaration(source, marker).replace('private func', 'func'))
  await verify('runtime-history-stubs.swift', [
    declaration(source, 'struct ModemDeckRuntimeDataWatermark'),
    `@MainActor final class Store {\n${runtimeStoreFields}\n${['func load()', 'private func finishWaitingLoads()', 'func clear()'].map(marker => declaration(source.slice(source.indexOf('final class ModemDeckCallsStore:')), marker)).join('\n')}\n}`,
    `@MainActor final class Controller {\n${methods.join('\n')}\n${runtimeControllerFields}\n}`
  ])
})

const runtimeControllerFields = `
  var phase = Phase.paired
  var runtimeForeground = true
  var runtimeEventStream: ModemDeckRuntimeCallStream?
  var runtimeEventGeneration = 0
  var runtimeReconnectWorkItem: DispatchWorkItem?
  var runtimeReconnectAttempts = 0
  var runtimeWatermark = ModemDeckRuntimeDataWatermark()
  var runtimeCollectionsTask: Task<Void, Never>?
  var runtimeCollectionsGeneration = 0
  var runtimeReloadRequested = false
  let credentialStore = CredentialStore()
  let contactsStore = Store()
  let messagesStore = Store()
  let callsStore = Store()
  var revoked = false
  func handleAuthenticationFailure() async { revoked = true; phase = .unpaired }
`

test('opened call and recording details resolve updates through their existing collection owner', { skip: process.platform !== 'darwin' }, async () => {
  const source = await readFile(new URL('../ios/App/App/ModemDeckCallViews.swift', import.meta.url), 'utf8')
  await verify('runtime-detail-stubs.swift', [
    `struct CallDetail { let initialCall: ModemDeckCallRecord; let store: Store;\n${['private var call: ModemDeckCallRecord', 'private var recordings: [ModemDeckRecording]'].map(marker => declaration(source, marker).replace('private var', 'var')).join('\n')}\n}`,
    `struct RecordingDetail { let initialRecording: ModemDeckRecording; let store: Store;\n${declaration(source, 'private var recording: ModemDeckRecording').replace('private var', 'var')}\n}`
  ])
})

const runtimeStoreFields = `
  var calls: [Int] = [], recordings: [Int] = []
  var loading = false, lastLoadSucceeded = false, reloadRequested = false
  var revision = 0, scope = 0
  var errorMessage = ""
  var reloadWaiters: [CheckedContinuation<Void, Never>] = []
  let api = HistoryAPI()
  var loads: Int { api.loads }
  var fail: Bool { get { api.fail } set { api.fail = newValue } }
  var block: Bool { get { api.block } set { api.block = newValue } }
  var continuation: CheckedContinuation<Void, Never>? { api.gates.values.first }
  func release() { api.block = false; let gates = api.gates; api.gates.removeAll(); gates.values.forEach { $0.resume() } }
`


test('pinned collection requests cancel old pairing responses without revoking a replacement or changing verification semantics', { skip: process.platform !== 'darwin' }, async () => {
  const source = await readFile(new URL('../ios/App/App/ModemDeckAPI.swift', import.meta.url), 'utf8')
  await verify('pairing-request-stubs.swift', ['func verify(', 'private func decode<', 'private func data(',
    'private func isCurrentCredential(', 'private func reportConnectivity('].map(marker => declaration(source, marker).replace('private func', 'func')))
})


test('call end presentation separates local media stop from confirmation, freezes duration and cannot dismiss a replacement call', { skip: process.platform !== 'darwin' }, async () => {
  const source = await readFile(new URL('../ios/App/App/ModemDeckSession.swift', import.meta.url), 'utf8')
  await verify('call-end-presentation-stubs.swift', [
    declaration(source, 'struct ModemDeckPresentedCall:'), declaration(source, 'struct ModemDeckEndedCall:'),
    `@MainActor final class Controller {\n${endingControllerFields}\n${['nonisolated func callStateDidChange(', 'nonisolated func callTerminationDidChange(',
      'private func applyCallTermination(', 'private func presentEndedCall(', 'private func scheduleEndedCallDismissal()',
      'func closeEndedCall()', 'func dismissEndedCall()', 'func end()', 'private func resetCallState()', 'private func prepareForNewCall(']
      .map(marker => declaration(source, marker).replace('private func', 'func')).join('\n')}\n}`
  ])
})

const endingControllerFields = `
 var call: ModemDeckPresentedCall?, endedCall: ModemDeckEndedCall?
 var endedCallClosing = false, busy = false, ending = false, muteBusy = false, recordingBusy = false, dtmfBusy = false
 var recordingEnabled = false, recordingReady = false
 var recordingStatus = "off", errorMessage = "", connectionFailureMessage = "", testCallResult = "", recordingSnapshotCallID = ""
 var recordingGeneration = 0, endedCallGeneration = 0
 var endedCallDismissWorkItem: DispatchWorkItem?
 var localEndRequestedCallID: String?
 var lastTerminationStatus: (callID: String, status: String)?
 var dtmfQueue: [String] = []
 var visibleCall: ModemDeckPresentedCall? { call ?? endedCall?.call }
 func reconcileRecording(for call: ModemDeckPresentedCall) { recordingReady = true }
`

test('native termination owner publishes pending after local teardown and confirms only its original pairing', { skip: process.platform !== 'darwin' }, async () => {
  const source = await readFile(new URL('../ios/App/App/ModemDeckNative.swift', import.meta.url), 'utf8')
  await verify('call-end-owner-stubs.swift', [declaration(source, 'private struct ModemDeckCallEndIntent:'),
    declaration(source, 'private final class ModemDeckCallEndOperation'),
    `private final class Coordinator {\n${endingOwnerFields}\n${['private func installPendingCallEnd(', 'private func publishCallTermination(', 'private func cleanupCall(']
      .map(marker => declaration(source, marker).replace('private func', 'func')).join('\n')}\n}`
  ])
})

const endingOwnerFields = `
 struct CallControlTarget { let callID: String; let credential: ModemDeckCredential }
 var credential = ModemDeckCredential(token: "old")
 var pendingCallEnds: [UUID: ModemDeckCallEndOperation] = [:]
 var pendingEndIntents: [UUID: ModemDeckCallEndIntent] = [:]
 var answerRequestsInFlight = Set<UUID>()
 var callIDsByUUID: [UUID: String] = [:]
 var presentedCalls: [UUID: PresentedCall] = [:]
 var audioTestCallUUIDs = Set<UUID>(), testCallUUIDs = Set<UUID>()
 var answeredCallUUIDs = Set<UUID>(), answerRequestedCallUUIDs = Set<UUID>()
 var outgoingCallUUIDs = Set<UUID>(), mutedCallUUIDs = Set<UUID>()
 var preferredRecordingByUUID: [UUID: Bool] = [:]
 var callAudioSessions: [UUID: Audio] = [:]
 var testCallTimeouts: [UUID: DispatchWorkItem] = [:]
 let testCallTone = Audio()
 var callStateObserver: Observer? = Observer()
 var readCompletion: ((Result<ModemDeckCallEndOperation.Observation, Error>) -> Void)?
 var saves = 0, backgrounds = 0
 func loadCredential() -> ModemDeckCredential? { credential }
 func sendCallAction(verb: String, callUUID: UUID, operationID: String, target: CallControlTarget, completion: @escaping (Result<Void, Error>) -> Void) { completion(.success(())) }
 func readCallEndState(target: CallControlTarget, completion: @escaping (Result<ModemDeckCallEndOperation.Observation, Error>) -> Void) { readCompletion = completion }
 func savePendingEndIntents() { saves += 1 }
 func beginCallEndBackgroundTask(_ uuid: UUID) { backgrounds += 1 }
 func finishCallEndBackgroundTask(_ uuid: UUID) { backgrounds -= 1 }
 func settleProviderCallActions(for uuid: UUID) {}
 func failRequestedCallActions(_ error: Error, for uuid: UUID) {}
 func rememberEndedCall(_ uuid: UUID) {}
 func stopRuntimeCallStream() {}
 func currentCallStatePayload() -> [String: Any] { ["state": "idle"] }
 func publishCallState() { callStateObserver?.callStateDidChange(currentCallStatePayload()) }
`


test('late mute, DTMF and recording completions cannot change a replacement call', { skip: process.platform !== 'darwin' }, async () => {
  const source = await readFile(new URL('../ios/App/App/ModemDeckSession.swift', import.meta.url), 'utf8')
  await verify('call-control-scope-stubs.swift', [
    `@MainActor final class Controller {\n${controlScopeFields}\n${['private struct DTMFRequest:', 'func setMuted(', 'func enqueueDTMF(', 'private func drainDTMFQueue()',
      'func toggleRecording()', 'private func acceptRecordingState(', 'private func prepareForNewCall('].map(marker => declaration(source, marker).replace('private struct', 'struct').replace('private func', 'func')).join('\n')}\n}`
  ])
})

const controlScopeFields = `
 var call: Call? = Call(callID: "old")
 var busy = false, ending = false, muteBusy = false, dtmfBusy = false, recordingBusy = false, recordingReady = true, recordingEnabled = false
 var recordingGeneration = 0, recordingStatus = "off", errorMessage = "", recordingSnapshotCallID = ""
 var dtmfQueue: [DTMFRequest] = []
 let api = RecordingAPI()
 var recordingReads = 0
 func refreshRecordingState(callID: String) async { recordingReads += 1 }
 func reconcileRecording(for call: ModemDeckPresentedCall) { recordingReady = true }
`
