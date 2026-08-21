import { spawn } from 'node:child_process'
import {
  access,
  cp,
  copyFile,
  mkdir,
  readFile,
  rm,
  symlink,
  writeFile
} from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const iosRoot = path.dirname(path.dirname(fileURLToPath(import.meta.url)))
const webSource = path.resolve(iosRoot, '../web')
const previewSource = path.join(iosRoot, '.web-preview')
const outputDirectory = path.join(iosRoot, 'www')
const webNodeModules = path.join(webSource, 'node_modules')
const viteBinary = path.join(webNodeModules, '.bin', 'vite')
const nativeStyles = path.join(iosRoot, 'assets/native-ios.css')
const nativeOnboardingStyles = path.join(iosRoot, 'assets/native-onboarding.css')
const nativeBridgeSource = path.join(iosRoot, 'assets/native-ios.ts')
const nativeNavigationSource = path.join(
  iosRoot,
  'assets/nativeIOSNavigation.ts'
)
const nativeResponseSource = path.join(iosRoot, 'assets/nativeResponse.ts')
const nativeContactImportSource = path.join(
  iosRoot,
  'assets/NativeContactImportButton.vue'
)
const nativeSettingsSource = path.join(
  iosRoot,
  'assets/NativeIOSSettingsPanel.vue'
)

function replaceExactly(source, search, replacement, filePath) {
  const occurrences = source.split(search).length - 1
  if (occurrences !== 1) {
    throw new Error(`Expected one native iOS patch target in ${filePath}, found ${occurrences}`)
  }
  return source.replace(search, replacement)
}

function run(command, args, options = {}) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, {
      cwd: options.cwd,
      env: options.env,
      stdio: 'inherit'
    })
    child.once('error', reject)
    child.once('exit', code => {
      if (code === 0) resolve()
      else reject(new Error(`${command} exited with status ${code}`))
    })
  })
}

function shouldCopy(sourcePath) {
  const relativePath = path.relative(webSource, sourcePath)
  if (!relativePath) return true
  const firstComponent = relativePath.split(path.sep)[0]
  return !['node_modules', 'dist', '.git'].includes(firstComponent)
}

await access(path.join(webSource, 'package.json'))
await access(viteBinary)
await access(nativeStyles)
await access(nativeOnboardingStyles)
await access(nativeBridgeSource)
await access(nativeNavigationSource)
await access(nativeResponseSource)
await access(nativeContactImportSource)
await access(nativeSettingsSource)

await rm(previewSource, { recursive: true, force: true })
await rm(outputDirectory, { recursive: true, force: true })
await mkdir(previewSource, { recursive: true })
await cp(webSource, previewSource, {
  recursive: true,
  filter: shouldCopy
})
await symlink(webNodeModules, path.join(previewSource, 'node_modules'), 'dir')

const nativeBridgeTarget = path.join(previewSource, 'src/api/nativeIOS.ts')
await copyFile(nativeBridgeSource, nativeBridgeTarget)
await copyFile(
  nativeNavigationSource,
  path.join(previewSource, 'src/api/nativeIOSNavigation.ts')
)
await copyFile(nativeResponseSource, path.join(previewSource, 'src/api/nativeResponse.ts'))
await copyFile(
  nativeContactImportSource,
  path.join(previewSource, 'src/components/NativeContactImportButton.vue')
)
await copyFile(
  nativeSettingsSource,
  path.join(previewSource, 'src/components/NativeIOSSettingsPanel.vue')
)

const mainPath = path.join(previewSource, 'src/main.ts')
let mainSource = await readFile(mainPath, 'utf8')
mainSource = replaceExactly(
  mainSource,
  "import './style.css'",
  [
    "import './style.css'",
    "import { ensureNativeIOSConfiguration } from './api/nativeIOS'"
  ].join('\n'),
  mainPath
)
mainSource = replaceExactly(
  mainSource,
  'async function mount(): Promise<void> {',
  [
    'async function mount(): Promise<void> {',
    '  await ensureNativeIOSConfiguration()'
  ].join('\n'),
  mainPath
)
await writeFile(mainPath, mainSource, 'utf8')

const previewIndexPath = path.join(previewSource, 'index.html')
let previewIndexSource = await readFile(previewIndexPath, 'utf8')
previewIndexSource = replaceExactly(
  previewIndexSource,
  'content="width=device-width, initial-scale=1.0, viewport-fit=cover"',
  'content="width=device-width, initial-scale=1.0, maximum-scale=1.0, user-scalable=no, viewport-fit=cover"',
  previewIndexPath
)
await writeFile(previewIndexPath, previewIndexSource, 'utf8')

const appShellPath = path.join(previewSource, 'src/components/AppShell.vue')
let appShellSource = await readFile(appShellPath, 'utf8')
appShellSource = replaceExactly(
  appShellSource,
  "import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'",
  "import { computed, onBeforeUnmount, onMounted, provide, ref, watch } from 'vue'",
  appShellPath
)
appShellSource = replaceExactly(
  appShellSource,
  "import { openDialer, showCallSurface, uiState } from '../state/ui'",
  [
    "import { nativeIOSMode } from '../api/nativeIOS'",
    "import { nativeIOSMobileBackKey } from '../api/nativeIOSNavigation'",
    "import { openDialer, showCallSurface, uiState } from '../state/ui'"
  ].join('\n'),
  appShellPath
)
appShellSource = replaceExactly(
  appShellSource,
  [
    'const mobileTrafficFromSettings = computed(',
    "  () => route.name === 'traffic' && route.query.from === 'settings'",
    ')'
  ].join('\n'),
  [
    'const mobileTrafficFromSettings = computed(',
    "  () => route.name === 'traffic' && route.query.from === 'settings'",
    ')',
    'const nativeIOSSettingsHeaderVisible = computed(',
    '  () =>',
    "    route.name === 'settings' ||",
    '    mobileOverviewFromSettings.value ||',
    '    mobileTrafficFromSettings.value',
    ')'
  ].join('\n'),
  appShellPath
)
appShellSource = replaceExactly(
  appShellSource,
  '    <main class="shell-main">',
  [
    '    <main',
    '      class="shell-main"',
    "      :class=\"{ 'native-ios-settings-header-visible': nativeIOSSettingsHeaderVisible }\"",
    '    >'
  ].join('\n'),
  appShellPath
)
appShellSource = replaceExactly(
  appShellSource,
  'onMounted(() => {',
  [
    'if (nativeIOSMode) {',
    '  provide(nativeIOSMobileBackKey, handleMobileBack)',
    '}',
    '',
    'onMounted(() => {'
  ].join('\n'),
  appShellPath
)
appShellSource = replaceExactly(
  appShellSource,
  '      <header class="shell-header">',
  '      <header v-if="nativeIOSSettingsHeaderVisible" class="shell-header">',
  appShellPath
)
await writeFile(appShellPath, appShellSource, 'utf8')

const workspaceDetailHeaderPath = path.join(
  previewSource,
  'src/components/workspace/WorkspaceDetailHeader.vue'
)
let workspaceDetailHeaderSource = await readFile(
  workspaceDetailHeaderPath,
  'utf8'
)
workspaceDetailHeaderSource = replaceExactly(
  workspaceDetailHeaderSource,
  '<template>',
  [
    '<script setup lang="ts">',
    "import { inject } from 'vue'",
    "import { ArrowLeft } from '@lucide/vue'",
    "import { useI18n } from 'vue-i18n'",
    "import { nativeIOSMode } from '../../api/nativeIOS'",
    "import { nativeIOSMobileBackKey } from '../../api/nativeIOSNavigation'",
    '',
    'const { t } = useI18n()',
    'const nativeIOSMobileBack = nativeIOSMode',
    '  ? inject(nativeIOSMobileBackKey)',
    '  : undefined',
    '</script>',
    '',
    '<template>'
  ].join('\n'),
  workspaceDetailHeaderPath
)
workspaceDetailHeaderSource = replaceExactly(
  workspaceDetailHeaderSource,
  '    <slot name="leading" />',
  [
    '    <button',
    '      v-if="nativeIOSMobileBack && !$slots.leading"',
    '      class="icon-button workspace-detail-header__native-back"',
    '      type="button"',
    '      :title="t(\'nativeIOS.back\')"',
    '      :aria-label="t(\'nativeIOS.back\')"',
    '      @click="nativeIOSMobileBack"',
    '    >',
    '      <ArrowLeft :size="20" />',
    '    </button>',
    '    <slot name="leading" />'
  ].join('\n'),
  workspaceDetailHeaderPath
)
workspaceDetailHeaderSource = replaceExactly(
  workspaceDetailHeaderSource,
  '.workspace-detail-header__actions {',
  [
    '.workspace-detail-header__native-back {',
    '  display: none;',
    '  flex: 0 0 auto;',
    '}',
    '',
    '.workspace-detail-header__actions {'
  ].join('\n'),
  workspaceDetailHeaderPath
)
workspaceDetailHeaderSource = replaceExactly(
  workspaceDetailHeaderSource,
  '@media (max-width: 560px) {',
  [
    '@media (max-width: 860px) {',
    '  .workspace-detail-header__native-back {',
    '    display: inline-grid;',
    '  }',
    '}',
    '',
    '@media (max-width: 560px) {'
  ].join('\n'),
  workspaceDetailHeaderPath
)
await writeFile(workspaceDetailHeaderPath, workspaceDetailHeaderSource, 'utf8')

const clientPath = path.join(previewSource, 'src/api/client.ts')
let clientSource = await readFile(clientPath, 'utf8')
clientSource = replaceExactly(
  clientSource,
  "import { ApiError } from './types'",
  [
    "import { ApiError } from './types'",
    "import { createModemDeckEventSource, nativeIOSFetch } from './nativeIOS'"
  ].join('\n'),
  clientPath
)
clientSource = replaceExactly(
  clientSource,
  '  if (session.authenticated && (!session.username || !session.csrf_token)) {',
  '  if (session.authenticated && !session.username) {',
  clientPath
)
clientSource = replaceExactly(
  clientSource,
  [
    '  const timeoutSignal = AbortSignal.timeout(',
    '    timeoutMilliseconds ??',
    "      (method === 'GET' || method === 'HEAD' || method === 'OPTIONS'",
    '        ? READ_REQUEST_TIMEOUT_MS',
    '        : WRITE_REQUEST_TIMEOUT_MS)',
    '  )'
  ].join('\n'),
  [
    '  const effectiveTimeoutMilliseconds =',
    '    timeoutMilliseconds ??',
    "    (method === 'GET' || method === 'HEAD' || method === 'OPTIONS'",
    '      ? READ_REQUEST_TIMEOUT_MS',
    '      : WRITE_REQUEST_TIMEOUT_MS)',
    '  const timeoutSignal = AbortSignal.timeout(effectiveTimeoutMilliseconds)'
  ].join('\n'),
  clientPath
)
clientSource = replaceExactly(
  clientSource,
  "    response = await fetch(path, { ...init, headers, signal, credentials: 'same-origin' })",
  [
    '    response = await nativeIOSFetch(',
    '      path,',
    "      { ...init, headers, signal, credentials: 'same-origin' },",
    '      effectiveTimeoutMilliseconds',
    '    )'
  ].join('\n'),
  clientPath
)
clientSource = replaceExactly(
  clientSource,
  "    const current = new EventSource(path, { withCredentials: true })",
  '    const current = createModemDeckEventSource(path)',
  clientPath
)
await writeFile(clientPath, clientSource, 'utf8')

const callMediaPath = path.join(previewSource, 'src/state/callMedia.ts')
let callMediaSource = await readFile(callMediaPath, 'utf8')
callMediaSource = replaceExactly(
  callMediaSource,
  "import { fixtureCallMediaPreview, gateway } from '../api/client'",
  [
    "import { fixtureCallMediaPreview, gateway } from '../api/client'",
    "import { nativeIOSMode, setNativeIOSCallMuted } from '../api/nativeIOS'"
  ].join('\n'),
  callMediaPath
)
callMediaSource = replaceExactly(
  callMediaSource,
  'export function syncCallMedia(session: CallSession | null): void {\n  if (fixtureCallMediaPreview) {',
  [
    'export function syncCallMedia(session: CallSession | null): void {',
    '  if (nativeIOSMode) {',
    "    if (!session || session.phase !== 'active') {",
    "      setIdle('idle')",
    '      return',
    '    }',
    '    if (!session.media_available) {',
    "      setIdle('unavailable', session.id)",
    '      return',
    '    }',
    "    if (callMediaState.callID === session.id && callMediaState.status === 'active') return",
    "    setIdle('idle')",
    '    callMediaState.callID = session.id',
    "    callMediaState.status = 'active'",
    '    return',
    '  }',
    '  if (fixtureCallMediaPreview) {'
  ].join('\n'),
  callMediaPath
)
callMediaSource = replaceExactly(
  callMediaSource,
  [
    'export function retryCallMedia(session: CallSession | null): void {',
    "  if (!session || session.phase !== 'active' || !session.media_available) return"
  ].join('\n'),
  [
    'export function retryCallMedia(session: CallSession | null): void {',
    '  if (nativeIOSMode) {',
    '    syncCallMedia(session)',
    '    return',
    '  }',
    "  if (!session || session.phase !== 'active' || !session.media_available) return"
  ].join('\n'),
  callMediaPath
)
callMediaSource = replaceExactly(
  callMediaSource,
  'export function toggleCallMute(): void {\n  if (fixtureCallMediaPreview) {',
  [
    'export function toggleCallMute(): void {',
    '  if (nativeIOSMode) {',
    "    if (callMediaState.status !== 'active') return",
    '    const callID = callMediaState.callID',
    '    const previous = callMediaState.muted',
    '    const muted = !previous',
    '    callMediaState.muted = muted',
    '    void setNativeIOSCallMuted(muted).catch(() => {',
    '      if (callMediaState.callID === callID && callMediaState.muted === muted) {',
    '        callMediaState.muted = previous',
    '      }',
    '    })',
    '    return',
    '  }',
    '  if (fixtureCallMediaPreview) {'
  ].join('\n'),
  callMediaPath
)
await writeFile(callMediaPath, callMediaSource, 'utf8')

const callStatePath = path.join(previewSource, 'src/state/call.ts')
let callStateSource = await readFile(callStatePath, 'utf8')
callStateSource = replaceExactly(
  callStateSource,
  "import { gateway } from '../api/client'",
    [
      "import { gateway } from '../api/client'",
      'import {',
      '  answerNativeIOSCall,',
      '  endNativeIOSCall,',
      '  listenNativeIOSCallState,',
      '  nativeIOSMode,',
      '  readNativeIOSCallState,',
      '  sendNativeIOSCallDTMF,',
      '  startNativeIOSOutgoingCall,',
      '  type NativeIOSCallState',
      "} from '../api/nativeIOS'"
    ].join('\n'),
  callStatePath
)
callStateSource = replaceExactly(
  callStateSource,
  '  syncCallRecording(owned || incomingAvailable ? session : null)',
  [
    '  syncCallRecording(',
    '    (owned || incomingAvailable) && !isNativeIOSTestCallSession(session)',
    '      ? session',
    '      : null',
    '  )'
  ].join('\n'),
  callStatePath
)
callStateSource = replaceExactly(
  callStateSource,
  [
    'function sessionCanRenewBrowserLease(session: CallSession): boolean {',
    '  return LEASED_PHASES.has(session.phase)',
    '}'
  ].join('\n'),
  [
    'function sessionCanRenewBrowserLease(session: CallSession): boolean {',
    '  return !isNativeIOSTestCallSession(session) && LEASED_PHASES.has(session.phase)',
    '}'
  ].join('\n'),
  callStatePath
)
callStateSource = replaceExactly(
  callStateSource,
  'export async function retryActiveCallMedia(session: CallSession | null): Promise<void> {\n  if (!session || session.phase !== \'active\' || !session.media_available) return',
  [
    'export async function retryActiveCallMedia(session: CallSession | null): Promise<void> {',
    '  if (isNativeIOSTestCallSession(session)) {',
    '    retryCallMedia(session)',
    '    return',
    '  }',
    "  if (!session || session.phase !== 'active' || !session.media_available) return"
  ].join('\n'),
  callStatePath
)
callStateSource = replaceExactly(
  callStateSource,
  'const notifiedIncomingCallIDs = new Set<string>()',
  [
    'const notifiedIncomingCallIDs = new Set<string>()',
    'let nativeIOSCallState: NativeIOSCallState = { state: \'idle\' }',
    'let removeNativeIOSCallStateListener: (() => void) | undefined',
    'let lastNativeIOSTestCallID = \'\'',
    'let latestServerActiveSnapshot: ActiveCallSnapshot = { calls: [], reservations: [] }'
  ].join('\n'),
  callStatePath
)
callStateSource = replaceExactly(
  callStateSource,
  [
    'function reconcileActiveSnapshot(',
    '  snapshot: ActiveCallSnapshot,',
    '  preferredCallID = callState.selectedCallID',
    '): void {',
    '  const liveSessions = snapshot.calls.filter(isLiveCallSession)',
    '  callState.sessions = liveSessions',
    '  callState.reservations = snapshot.reservations.slice()'
  ].join('\n'),
  [
    'export function isNativeIOSTestCallSession(session: CallSession | null): boolean {',
    "  return nativeIOSMode && session?.bearer === 'native-test'",
    '}',
    '',
    'function nativeIOSCallSession(): CallSession | null {',
    '  const state = nativeIOSCallState',
    "  if (state.state === 'idle' || !state.callID) return null",
    '  const existing = latestServerActiveSnapshot.calls.find(',
    '    session => session.id === state.callID',
    '  )',
    '  const now = new Date().toISOString()',
    '  const phase =',
    "    state.state === 'ringing'",
    "      ? 'ringing'",
    "      : state.state === 'active'",
    "        ? 'active'",
    "        : 'connecting'",
    '  return {',
    '    ...existing,',
    '    id: state.callID,',
    "    line_id: existing?.line_id || '',",
    "    direction: state.direction || existing?.direction || 'incoming',",
    "    remote_number: state.remoteNumber || existing?.remote_number || '',",
    '    display_name: state.displayName || existing?.display_name || state.remoteNumber,',
    '    phase,',
    "    control_state: state.state === 'ringing' ? 'available' : 'owned',",
    '    media_available: state.testCall ? true : (existing?.media_available ?? true),',
    '    created_at: state.createdAt || existing?.created_at || now,',
    "    active_at: state.state === 'active' ? (state.activeAt || existing?.active_at || now) : existing?.active_at,",
    "    bearer: state.testCall ? 'native-test' : existing?.bearer",
    '  }',
    '}',
    '',
    'function activeSnapshotWithNativeIOSCall(',
    '  snapshot: ActiveCallSnapshot',
    '): ActiveCallSnapshot {',
    '  const calls = snapshot.calls.filter(',
    '    session => !lastNativeIOSTestCallID || session.id !== lastNativeIOSTestCallID',
    '  )',
    '  const nativeSession = nativeIOSCallSession()',
    '  if (nativeSession) {',
    '    const index = calls.findIndex(session => session.id === nativeSession.id)',
    '    if (index >= 0) calls[index] = nativeSession',
    '    else calls.push(nativeSession)',
    '  }',
    '  return { calls, reservations: snapshot.reservations.slice() }',
    '}',
    '',
    'function acceptNativeIOSCallState(state: NativeIOSCallState): void {',
    '  nativeIOSCallState = state',
    '  if (state.testCall && state.callID) lastNativeIOSTestCallID = state.callID',
    '  if (!runtimeStarted) return',
    '  mutationEpoch += 1',
    '  reconcileActiveSnapshot(',
    '    latestServerActiveSnapshot,',
    "    state.state === 'idle' ? '' : state.callID || callState.selectedCallID,",
    '    false',
    '  )',
    "  if (state.state === 'idle') void requestActiveCallRefresh()",
    '}',
    '',
    'function reconcileActiveSnapshot(',
    '  snapshot: ActiveCallSnapshot,',
    '  preferredCallID = callState.selectedCallID,',
    '  rememberServerSnapshot = true',
    '): void {',
    '  if (rememberServerSnapshot) {',
    '    latestServerActiveSnapshot = {',
    '      calls: snapshot.calls.filter(',
    '        session => !lastNativeIOSTestCallID || session.id !== lastNativeIOSTestCallID',
    '      ),',
    '      reservations: snapshot.reservations.slice()',
    '    }',
    '  }',
    '  const mergedSnapshot = activeSnapshotWithNativeIOSCall(latestServerActiveSnapshot)',
    '  const liveSessions = mergedSnapshot.calls.filter(isLiveCallSession)',
    '  callState.sessions = liveSessions',
    '  callState.reservations = mergedSnapshot.reservations.slice()'
  ].join('\n'),
  callStatePath
)
callStateSource = replaceExactly(
  callStateSource,
  [
    'function showIncomingCallNotification(session: CallSession): void {',
    '  if (!activeRouter || !claimIncomingCallNotification(session, notifiedIncomingCallIDs)) return'
  ].join('\n'),
  [
    'function showIncomingCallNotification(session: CallSession): void {',
    '  if (nativeIOSMode) return',
    '  if (!activeRouter || !claimIncomingCallNotification(session, notifiedIncomingCallIDs)) return'
  ].join('\n'),
  callStatePath
)
callStateSource = replaceExactly(
  callStateSource,
  [
    '    const session = await gateway.startCall(lineKey, number, recordingEnabled)',
    '    acceptSession(session)'
  ].join('\n'),
  [
    '    const session = await gateway.startCall(lineKey, number, recordingEnabled)',
    '    if (nativeIOSMode) {',
    '      try {',
    '        await startNativeIOSOutgoingCall({',
    '          callID: session.id,',
    '          remoteNumber: session.remote_number || number,',
    '          displayName: session.display_name || session.remote_number || number',
    '        })',
    '      } catch (error) {',
    "        await gateway.callAction(session.id, 'hangup').catch(() => undefined)",
    '        throw error',
    '      }',
    '    }',
    '    acceptSession(session)'
  ].join('\n'),
  callStatePath
)
callStateSource = replaceExactly(
  callStateSource,
  [
    '    await gateway.callAction(',
    '      id,',
    '      action,',
    "      action === 'answer' ? preferredCallRecording(id) : undefined",
    '    )',
    "    if (action === 'answer' && previousSession) {"
  ].join('\n'),
  [
    '    if (nativeIOSMode) {',
    "      if (action === 'answer') await answerNativeIOSCall(id)",
    '      else await endNativeIOSCall(id)',
    '    } else {',
    '      await gateway.callAction(',
    '        id,',
    '        action,',
    "        action === 'answer' ? preferredCallRecording(id) : undefined",
    '      )',
    '    }',
    "    if (!nativeIOSMode && action === 'answer' && previousSession) {"
  ].join('\n'),
  callStatePath
)
callStateSource = replaceExactly(
  callStateSource,
  '    await gateway.sendDTMF(id, digit)',
  [
    '    if (nativeIOSMode) await sendNativeIOSCallDTMF(id, digit)',
    '    else await gateway.sendDTMF(id, digit)'
  ].join('\n'),
  callStatePath
)
callStateSource = replaceExactly(
  callStateSource,
  [
    "  if (typeof document !== 'undefined') {",
    "    document.addEventListener('visibilitychange', resumeVisibleCallRuntime)",
    '  }',
    '  void refreshActiveCalls()',
    '}'
  ].join('\n'),
  [
    "  if (typeof document !== 'undefined') {",
    "    document.addEventListener('visibilitychange', resumeVisibleCallRuntime)",
    '  }',
    '  if (nativeIOSMode) {',
    '    void listenNativeIOSCallState(acceptNativeIOSCallState)',
    '      .then(remove => {',
    '        if (!runtimeStarted) {',
    '          remove()',
    '          return',
    '        }',
    '        removeNativeIOSCallStateListener?.()',
    '        removeNativeIOSCallStateListener = remove',
    '        return readNativeIOSCallState()',
    '      })',
    '      .then(state => {',
    '        if (state && runtimeStarted) acceptNativeIOSCallState(state)',
    '      })',
    '      .catch(() => undefined)',
    '  }',
    '  void refreshActiveCalls()',
    '}'
  ].join('\n'),
  callStatePath
)
callStateSource = replaceExactly(
  callStateSource,
  [
    "  if (typeof document !== 'undefined') {",
    "    document.removeEventListener('visibilitychange', resumeVisibleCallRuntime)",
    '  }',
    '  syncCallSounds(null)'
  ].join('\n'),
  [
    "  if (typeof document !== 'undefined') {",
    "    document.removeEventListener('visibilitychange', resumeVisibleCallRuntime)",
    '  }',
    '  removeNativeIOSCallStateListener?.()',
    '  removeNativeIOSCallStateListener = undefined',
    "  nativeIOSCallState = { state: 'idle' }",
    "  lastNativeIOSTestCallID = ''",
    '  latestServerActiveSnapshot = { calls: [], reservations: [] }',
    '  syncCallSounds(null)'
  ].join('\n'),
  callStatePath
)
await writeFile(callStatePath, callStateSource, 'utf8')

const callSurfacePath = path.join(previewSource, 'src/components/CallSurface.vue')
let callSurfaceSource = await readFile(callSurfacePath, 'utf8')
callSurfaceSource = replaceExactly(
  callSurfaceSource,
  '  hangupCall,\n  rejectCall,',
  '  hangupCall,\n  isNativeIOSTestCallSession,\n  rejectCall,',
  callSurfacePath
)
callSurfaceSource = replaceExactly(
  callSurfaceSource,
  'const session = computed(() => callState.session)',
  [
    'const session = computed(() => callState.session)',
    'const nativeIOSTestCall = computed(() => isNativeIOSTestCallSession(session.value))'
  ].join('\n'),
  callSurfacePath
)
callSurfaceSource = replaceExactly(
  callSurfaceSource,
  '  return (\n    !occupied.value &&',
  '  return (\n    !nativeIOSTestCall.value &&\n    !occupied.value &&',
  callSurfacePath
)
callSurfaceSource = replaceExactly(
  callSurfaceSource,
  'const bearerLabel = computed(() => {\n  const bearer = session.value?.bearer?.trim().toLocaleLowerCase()',
  [
    'const bearerLabel = computed(() => {',
    "  if (nativeIOSTestCall.value) return ''",
    '  const bearer = session.value?.bearer?.trim().toLocaleLowerCase()'
  ].join('\n'),
  callSurfacePath
)
callSurfaceSource = replaceExactly(
  callSurfaceSource,
  'const answerUnavailable = computed(() => {\n  if (lineSupports(line.value, \'answer\') === false) return t(\'calls.answerUnsupported\')',
  [
    'const answerUnavailable = computed(() => {',
    "  if (nativeIOSTestCall.value) return ''",
    "  if (lineSupports(line.value, 'answer') === false) return t('calls.answerUnsupported')"
  ].join('\n'),
  callSurfacePath
)
callSurfaceSource = replaceExactly(
  callSurfaceSource,
  "const dtmfUnavailable = computed(() =>\n  lineSupports(line.value, 'dtmf') === false ? t('calls.dtmfUnsupported') : ''\n)",
  [
    'const dtmfUnavailable = computed(() =>',
    "  nativeIOSTestCall.value || lineSupports(line.value, 'dtmf') === false",
    "    ? t('calls.dtmfUnsupported')",
    "    : ''",
    ')'
  ].join('\n'),
  callSurfacePath
)
callSurfaceSource = replaceExactly(
  callSurfaceSource,
  "const mediaLabel = computed(() => {\n  if (!active.value || !callState.owned) return ''",
  [
    'const mediaLabel = computed(() => {',
    "  if (!active.value || !callState.owned || nativeIOSTestCall.value) return ''"
  ].join('\n'),
  callSurfacePath
)
callSurfaceSource = replaceExactly(
  callSurfaceSource,
  '            <small v-else>{{ session.line_id }}</small>',
  '            <small v-else-if="session.line_id">{{ session.line_id }}</small>',
  callSurfacePath
)
await writeFile(callSurfacePath, callSurfaceSource, 'utf8')

const notificationPath = path.join(previewSource, 'src/state/browserNotifications.ts')
let notificationSource = await readFile(notificationPath, 'utf8')
notificationSource = replaceExactly(
  notificationSource,
  "import { translate } from '../i18n'",
  [
    "import { translate } from '../i18n'",
    "import { nativeIOSMode, showNativeIOSNotification } from '../api/nativeIOS'"
  ].join('\n'),
  notificationPath
)
notificationSource = replaceExactly(
  notificationSource,
  [
    'export function showBrowserNotification(input: {',
    '  title: string',
    '  body: string',
    '  tag: string',
    '  onClick: () => void',
    '}): boolean {',
    '  syncBrowserNotificationState()'
  ].join('\n'),
  [
    'export function showBrowserNotification(input: {',
    '  title: string',
    '  body: string',
    '  tag: string',
    '  onClick: () => void',
    '}): boolean {',
    '  if (nativeIOSMode) {',
    '    void showNativeIOSNotification({',
    '      title: input.title,',
    '      body: input.body,',
    '      identifier: input.tag',
    '    }).catch(() => undefined)',
    '    return true',
    '  }',
    '  syncBrowserNotificationState()'
  ].join('\n'),
  notificationPath
)
await writeFile(notificationPath, notificationSource, 'utf8')

const contractPath = path.join(previewSource, 'src/api/contract.ts')
let contractSource = await readFile(contractPath, 'utf8')
contractSource = replaceExactly(
  contractSource,
  "import { parseCallRecord, parseMessage, parsePageMeta } from './normalize.ts'",
  [
    "import { parseCallRecord, parseMessage, parsePageMeta } from './normalize.ts'",
    "import { nativeIOSAuthenticatedResourceURL } from './nativeIOS'"
  ].join('\n'),
  contractPath
)
contractSource = replaceExactly(
  contractSource,
  '  return `${parsed.pathname}${parsed.search}`',
  '  return nativeIOSAuthenticatedResourceURL(`${parsed.pathname}${parsed.search}`)',
  contractPath
)
const directRecordingPath = 'download_url: `/api/v1/calls/${encodeURIComponent(callID)}/recordings/${encodeURIComponent(id)}/download`'
const directRecordingOccurrences = contractSource.split(directRecordingPath).length - 1
if (directRecordingOccurrences !== 1) {
  throw new Error(`Expected one native recording URL patch target in ${contractPath}, found ${directRecordingOccurrences}`)
}
contractSource = contractSource.replaceAll(
  directRecordingPath,
  'download_url: nativeIOSAuthenticatedResourceURL(`/api/v1/calls/${encodeURIComponent(callID)}/recordings/${encodeURIComponent(id)}/download`)'
)
contractSource = replaceExactly(
  contractSource,
  'result.download_url = `/api/v1/calls/${encodeURIComponent(callID)}/recordings/${encodeURIComponent(id)}/download`',
  'result.download_url = nativeIOSAuthenticatedResourceURL(`/api/v1/calls/${encodeURIComponent(callID)}/recordings/${encodeURIComponent(id)}/download`)',
  contractPath
)
await writeFile(contractPath, contractSource, 'utf8')

const environmentPath = path.join(previewSource, 'src/env.d.ts')
let environmentSource = await readFile(environmentPath, 'utf8')
environmentSource = replaceExactly(
  environmentSource,
  '  readonly VITE_MODEMDECK_FIXTURE?: string',
  [
    '  readonly VITE_MODEMDECK_FIXTURE?: string',
    '  readonly VITE_MODEMDECK_NATIVE_IOS?: string'
  ].join('\n'),
  environmentPath
)
await writeFile(environmentPath, environmentSource, 'utf8')

const dialerPath = path.join(previewSource, 'src/components/DialerPanel.vue')
const focusNumber = [
  'function focusNumber(): void {',
  '  inputAutofocus.value = false'
].join('\n')
const nativeFocusNumber = [
  'function focusNumber(): void {',
  "  if (import.meta.env.VITE_MODEMDECK_NATIVE_IOS === '1') return",
  '  inputAutofocus.value = false'
].join('\n')
const dialerSource = await readFile(dialerPath, 'utf8')
const focusOccurrences = dialerSource.split(focusNumber).length - 1
if (focusOccurrences !== 1) {
  throw new Error(`Expected one dialer focus hook in ${dialerPath}, found ${focusOccurrences}`)
}
await writeFile(
  dialerPath,
  dialerSource.replace(focusNumber, nativeFocusNumber),
  'utf8'
)

const contactSyncPath = path.join(previewSource, 'src/components/ContactSyncSettings.vue')
let contactSyncSource = await readFile(contactSyncPath, 'utf8')
contactSyncSource = replaceExactly(
  contactSyncSource,
  "import SettingsModuleCard from './settings/SettingsModuleCard.vue'",
  [
    "import SettingsModuleCard from './settings/SettingsModuleCard.vue'",
    "import NativeContactImportButton from './NativeContactImportButton.vue'"
  ].join('\n'),
  contactSyncPath
)
contactSyncSource = replaceExactly(
  contactSyncSource,
  [
    '        <div class="contact-sync-actions">',
    '          <button',
    '            class="primary-button"',
    '            type="button"',
    '            :disabled="vcardBusy"',
    '            @click="chooseVCard"'
  ].join('\n'),
  [
    '        <div class="contact-sync-actions">',
    '          <NativeContactImportButton',
    '            :disabled="vcardBusy || !initialResourcesReady"',
    '          />',
    '          <button',
    '            class="primary-button"',
    '            type="button"',
    '            :disabled="vcardBusy"',
    '            @click="chooseVCard"'
  ].join('\n'),
  contactSyncPath
)
await writeFile(contactSyncPath, contactSyncSource, 'utf8')

const dashboardPath = path.join(previewSource, 'src/views/DashboardView.vue')
let dashboardSource = await readFile(dashboardPath, 'utf8')
dashboardSource = replaceExactly(
  dashboardSource,
  '      <button\n        class="list-item dashboard-overview-row"',
  '      <button\n        v-if="false"\n        class="list-item dashboard-overview-row"',
  dashboardPath
)
dashboardSource = replaceExactly(
  dashboardSource,
  '      <div class="dashboard-list-label">{{ t(\'dashboard.recentActivity\') }}</div>',
  '      <div v-if="false" class="dashboard-list-label">{{ t(\'dashboard.recentActivity\') }}</div>',
  dashboardPath
)
await writeFile(dashboardPath, dashboardSource, 'utf8')

const preferencesPath = path.join(
  previewSource,
  'src/components/AccountPreferencesPanel.vue'
)
let preferencesSource = await readFile(preferencesPath, 'utf8')
preferencesSource = replaceExactly(
  preferencesSource,
  "import AccountSettingsPanel from './AccountSettingsPanel.vue'",
  [
    "import AccountSettingsPanel from './AccountSettingsPanel.vue'",
    "import NativeIOSSettingsPanel from './NativeIOSSettingsPanel.vue'"
  ].join('\n'),
  preferencesPath
)
preferencesSource = replaceExactly(
  preferencesSource,
  '  <AccountSettingsPanel :show-identity="false" />',
  [
    '  <AccountSettingsPanel :show-identity="false" />',
    '  <NativeIOSSettingsPanel />'
  ].join('\n'),
  preferencesPath
)
await writeFile(preferencesPath, preferencesSource, 'utf8')

const aboutPath = path.join(previewSource, 'src/components/AboutSettingsPanel.vue')
let aboutSource = await readFile(aboutPath, 'utf8')
aboutSource = replaceExactly(
  aboutSource,
  "import { gateway } from '../api/client'",
  [
    "import { gateway } from '../api/client'",
    "import { readNativeIOSAppInfo } from '../api/nativeIOS'"
  ].join('\n'),
  aboutPath
)
aboutSource = replaceExactly(
  aboutSource,
  'const about = ref<AboutInfo | null>(null)',
  [
    'const about = ref<AboutInfo | null>(null)',
    "const nativeAppVersion = ref('')"
  ].join('\n'),
  aboutPath
)
aboutSource = replaceExactly(
  aboutSource,
  [
    'const displayedVersion = computed(',
    "  () => update.value?.current_version || about.value?.version || t('about.notAvailable')",
    ')'
  ].join('\n'),
  [
    'const displayedVersion = computed(',
    "  () => nativeAppVersion.value || about.value?.version || t('about.notAvailable')",
    ')'
  ].join('\n'),
  aboutPath
)
aboutSource = replaceExactly(
  aboutSource,
  'async function load(): Promise<void> {',
  [
    'async function loadNativeAppInfo(): Promise<void> {',
    '  const info = await readNativeIOSAppInfo()',
    '  nativeAppVersion.value = info.build',
    '    ? `${info.version} (${info.build})`',
    '    : info.version',
    '}',
    '',
    'async function load(): Promise<void> {'
  ].join('\n'),
  aboutPath
)
aboutSource = replaceExactly(
  aboutSource,
  '  void waitForInitialLoad([() => load(), () => checkForUpdates()])',
  '  void waitForInitialLoad([() => load(), () => checkForUpdates(), () => loadNativeAppInfo()])',
  aboutPath
)
await writeFile(aboutPath, aboutSource, 'utf8')

await run(
  viteBinary,
  [
    'build',
    '--mode',
    'ios-uat',
    '--outDir',
    outputDirectory,
    '--emptyOutDir'
  ],
  {
    cwd: previewSource,
    env: {
      ...process.env,
      VITE_MODEMDECK_NATIVE_IOS: '1',
      VITE_MODEMDECK_BUILD_ID: 'ios-uat'
    }
  }
)

const indexPath = path.join(outputDirectory, 'index.html')
await access(indexPath)
await copyFile(nativeStyles, path.join(outputDirectory, 'native-ios.css'))
await copyFile(
  nativeOnboardingStyles,
  path.join(outputDirectory, 'native-onboarding.css')
)

const indexSource = await readFile(indexPath, 'utf8')
const headClose = '</head>'
const headCloseOccurrences = indexSource.split(headClose).length - 1
if (headCloseOccurrences !== 1) {
  throw new Error(`Expected one ${headClose} in ${indexPath}, found ${headCloseOccurrences}`)
}
await writeFile(
  indexPath,
  indexSource.replace(
    headClose,
    [
      '  <link rel="stylesheet" href="/native-ios.css">',
      '  <link rel="stylesheet" href="/native-onboarding.css">',
      `  ${headClose}`
    ].join('\n')
  ),
  'utf8'
)

await writeFile(
  path.join(outputDirectory, 'mobile-preview.json'),
  `${JSON.stringify({ source: '../web', mode: 'uat', authenticated: true })}\n`,
  'utf8'
)
