import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

async function source(path) {
  return readFile(new URL(path, import.meta.url), 'utf8')
}

function section(contents, start, end) {
  const startIndex = contents.indexOf(start)
  const endIndex = contents.indexOf(end, startIndex + start.length)
  assert.notEqual(startIndex, -1, `missing section start: ${start}`)
  assert.notEqual(endIndex, -1, `missing section end: ${end}`)
  return contents.slice(startIndex, endIndex)
}

test('initial page skeletons wait for every required request to settle', async () => {
  const [barrier, client, ...consumers] = await Promise.all([
    source('../src/composables/useInitialLoadBarrier.ts'),
    source('../src/api/client.ts'),
    source('../src/views/DashboardView.vue'),
    source('../src/views/ContactsView.vue'),
    source('../src/views/MessagesView.vue'),
    source('../src/views/CallsView.vue'),
    source('../src/views/RecordingsView.vue'),
    source('../src/views/TrafficView.vue'),
    source('../src/components/AccountSettingsPanel.vue'),
    source('../src/components/ContactSyncSettings.vue'),
    source('../src/components/AudioSettingsForm.vue'),
    source('../src/components/AboutSettingsPanel.vue'),
    source('../src/components/TelegramSettingsForm.vue'),
    source('../src/components/DeviceConfigurationPanel.vue'),
    source('../src/components/DiagnosticsPanel.vue')
  ])

  assert.match(barrier, /const loading = ref\(true\)/)
  assert.match(barrier, /await Promise\.allSettled/)
  assert.match(client, /const READ_REQUEST_TIMEOUT_MS = 15_000/)
  for (const consumer of consumers) {
    assert.match(consumer, /useInitialLoadBarrier/)
    assert.match(consumer, /waitForInitialLoad/)
  }
})

test('primary routes keep their final frame and use structure-specific skeletons', async () => {
  const [
    dashboard,
    contacts,
    messages,
    calls,
    recordings,
    traffic,
    devices,
    diagnostics,
    settingsShapes,
    preview,
    styles
  ] = await Promise.all([
    source('../src/views/DashboardView.vue'),
    source('../src/views/ContactsView.vue'),
    source('../src/views/MessagesView.vue'),
    source('../src/views/CallsView.vue'),
    source('../src/views/RecordingsView.vue'),
    source('../src/views/TrafficView.vue'),
    source('../src/components/DeviceConfigurationPanel.vue'),
    source('../src/components/DiagnosticsPanel.vue'),
    source('../src/components/settings/settingsSkeleton.ts'),
    source('../src/composables/useSkeletonPreview.ts'),
    source('../src/style.css')
  ])

  for (const route of [dashboard, contacts, messages, calls, recordings, traffic]) {
    assert.match(route, /LoadingSkeletonBoundary/)
    assert.doesNotMatch(route, /state="loading"/)
  }
  for (const detail of [dashboard, contacts, calls, recordings]) {
    assert.match(detail, /WorkspaceDetailSkeleton/)
  }
  assert.match(messages, /ConversationSkeleton/)
  assert.match(
    messages,
    /ConversationSkeleton :label="t\('messages\.loading'\)" framed/
  )
  assert.match(traffic, /TrafficSkeleton/)
  assert.match(devices, /SectionSkeleton/)
  assert.match(diagnostics, /SectionSkeleton/)
  assert.match(settingsShapes, /case 'preferences':\s*return 'preference-rows'/)
  assert.match(settingsShapes, /case 'pairing':\s*return 'modules-one'/)
  assert.match(settingsShapes, /case 'connectivity':\s*return 'connectivity'/)
  assert.match(preview, /fixtureMode/)
  assert.match(preview, /query\.get\('skeleton'\) === '1'/)
  assert.match(section(styles, '.messages-scroll {', '\n}'), /display:\s*flex/)
  assert.match(
    section(styles, '.messages-scroll {', '\n}'),
    /flex-direction:\s*column/
  )
  assert.match(section(styles, '.message-stack {', '\n}'), /margin-top:\s*auto/)
})

test('settings initial barriers cover every resource before revealing content', async () => {
  const [account, users, system, contacts, audio, audioDevices, about] = await Promise.all([
    source('../src/components/AccountSettingsPanel.vue'),
    source('../src/components/UserSettingsPanel.vue'),
    source('../src/components/SystemSettingsForm.vue'),
    source('../src/components/ContactSyncSettings.vue'),
    source('../src/components/AudioSettingsForm.vue'),
    source('../src/components/AudioDeviceControls.vue'),
    source('../src/components/AboutSettingsPanel.vue')
  ])

  assert.match(
    account,
    /waitForInitialLoad\(loaders\)[\s\S]*initialLoading && !accountResourcesReady/
  )
  assert.match(users, /gateway\.listUsers\(\)[\s\S]*loadBootstrap\(\)[\s\S]*loadContacts\(\)/)
  assert.match(system, /bootstrapResource\.data\?\.system_settings/)
  assert.match(system, /const bootstrap = await loadBootstrap\(\)/)
  assert.doesNotMatch(system, /gateway\.getSystemSettings\(\)/)
  assert.match(
    contacts,
    /waitForInitialLoad\(\[\(\) => loadBootstrap\(\), \(\) => loadContacts\(\)\]\)/
  )
  assert.match(
    audio,
    /waitForInitialLoad\(\[[\s\S]*refreshAudioDevices\(\)[\s\S]*loadRecordingSettings\(\)/
  )
  assert.match(audio, /<AudioDeviceControls :refresh-on-mount="false" \/>/)
  assert.match(audioDevices, /if \(props\.refreshOnMount\) void refreshAudioDevices\(\)/)
  assert.match(
    about,
    /waitForInitialLoad\(\[\(\) => load\(\), \(\) => checkForUpdates\(\)\]\)/
  )
  for (const panel of [account, contacts, audio, about]) {
    assert.match(panel, /<SettingsLoadBoundary/)
    assert.match(panel, /:loading=/)
  }
})

test('shared settings resources coalesce concurrent initial requests', async () => {
  const workspace = await source('../src/state/workspace.ts')

  for (const resource of ['bootstrap', 'contacts', 'devices', 'telegram']) {
    assert.match(workspace, new RegExp(`let ${resource}Load:`))
    assert.match(
      workspace,
      new RegExp(`if \\(${resource}Load\\) return ${resource}Load`)
    )
    assert.match(workspace, new RegExp(`${resource}Load = undefined`))
  }
})

test('device and diagnostics keep one exclusive skeleton until detail data settles', async () => {
  const [devices, diagnostics] = await Promise.all([
    source('../src/components/DeviceConfigurationPanel.vue'),
    source('../src/components/DiagnosticsPanel.vue')
  ])

  assert.match(
    devices,
    /async function loadInitialDeviceWorkspace\(\)[\s\S]*await Promise\.allSettled\([\s\S]*loadBootstrap\(\)[\s\S]*loadDevices\(\)[\s\S]*loadNetwork\(true, true\)[\s\S]*await nextTick\(\)[\s\S]*await loadDeviceConfiguration\(selectedLineID\.value\)/
  )
  assert.match(
    devices,
    /waitForInitialLoad\(\[\(\) => loadInitialDeviceWorkspace\(\)\]\)/
  )
  assert.match(
    diagnostics,
    /async function loadInitialDiagnostics\(\)[\s\S]*loadSnapshot\(\)[\s\S]*loadLogs\(\)[\s\S]*refreshAudioDevices\(\)[\s\S]*diagnosticLineIDs\.value\.map\(lineID =>[\s\S]*loadDiagnosticDeviceConfiguration\(lineID\)/
  )
  assert.match(
    diagnostics,
    /<SettingsLoadBoundary[\s\S]*:loading="[\s\S]*initialLoading \|\|[\s\S]*\(!snapshot && \(snapshotState === 'idle' \|\| snapshotState === 'loading'\)\)/
  )
  assert.match(devices, /<SettingsLoadBoundary[\s\S]*:loading="initialLoading"/)
  assert.match(diagnostics, /<template v-if="snapshot">/)
  assert.doesNotMatch(diagnostics, /<template v-else-if="snapshot">/)
  assert.match(
    diagnostics,
    /<section class="diagnostics-section log-section">[\s\S]*<\/SettingsLoadBoundary>/
  )
})

test('authenticated app startup requests microphone access once and releases the probe stream', async () => {
  const app = await source('../src/App.vue')
  const shell = await source('../src/components/AppShell.vue')
  const login = await source('../src/views/LoginView.vue')
  const audio = await source('../src/state/audio.ts')
  const initialize = section(
    audio,
    'export function initializeBrowserAudio(): Promise<void> {',
    'function onDeviceChange(): void {'
  )

  assert.doesNotMatch(app, /initializeBrowserAudio|shutdownAudioDevices/)
  assert.doesNotMatch(login, /initializeBrowserAudio|shutdownAudioDevices|getUserMedia/)
  assert.match(
    shell,
    /onMounted\(\(\) => \{[\s\S]*?void initializeBrowserAudio\(\)/
  )
  assert.match(
    shell,
    /onBeforeUnmount\(\(\) => \{[\s\S]*?shutdownAudioDevices\(\)/
  )
  assert.match(initialize, /if \(startupAccessAttempted\) return Promise\.resolve\(\)/)
  assert.match(initialize, /getUserMedia\(\{\s*audio: true,\s*video: false\s*\}\)/)
  assert.match(initialize, /temporaryStream\?\.getTracks\(\)/)
  assert.ok(
    initialize.indexOf('track.stop()') <
      initialize.indexOf('await refreshAudioDevicesAfterPermission()')
  )
  assert.match(
    audio,
    /'insecure-context'[\s\S]*'unsupported'[\s\S]*'prompt'[\s\S]*'pending'[\s\S]*'granted'[\s\S]*'denied'[\s\S]*'no-device'[\s\S]*'error'/
  )
})

test('microphone testing resumes AudioContext and stops after 30 seconds', async () => {
  const audio = await source('../src/state/audio.ts')
  const microphoneTest = section(
    audio,
    'export async function startMicrophoneTest(): Promise<void> {',
    '\n}'
  )

  assert.match(audio, /const MICROPHONE_TEST_LIMIT_MS = 30_000/)
  assert.match(microphoneTest, /getUserMedia/)
  assert.match(microphoneTest, /await context\.resume\(\)/)
  assert.match(microphoneTest, /window\.setTimeout\(\(\) => \{\s*stopMicrophoneTest\(\)/)
})

test('active calls apply output changes and replace input before stopping the old stream', async () => {
  const callMedia = await source('../src/state/callMedia.ts')
  const replaceInput = section(
    callMedia,
    'async function replaceCallInput(deviceID: string): Promise<void> {',
    'function queueCallInputReplacement(deviceID: string): void {'
  )

  const replaceTrackIndex = replaceInput.indexOf('await sender.replaceTrack(newTrack)')
  const commitStreamIndex = replaceInput.indexOf('localStream = replacement')
  const stopOldStreamIndex = replaceInput.indexOf(
    'stopMicrophonePipeline(pipeline)'
  )

  assert.ok(replaceTrackIndex >= 0)
  assert.ok(commitStreamIndex > replaceTrackIndex)
  assert.ok(stopOldStreamIndex > commitStreamIndex)
  assert.match(callMedia, /audioState\.selectedOutputID, audioState\.devicesRevision/)
  assert.match(callMedia, /if \(remoteAudio\) void playRemoteAudio\(\)/)
  assert.match(callMedia, /queueCallInputReplacement\(deviceID\)/)
  assert.match(callMedia, /session\.phase !== 'active'/)
  assert.match(callMedia, /!session\.media_available/)
  assert.match(
    callMedia,
    /connection\.connectionState === 'disconnected'\) \{[\s\S]*?callMediaState\.status = 'recovering'/
  )
  assert.doesNotMatch(
    callMedia,
    /connection\.connectionState === 'disconnected'\) \{\s*failConnection\(/
  )
})

test('desktop shell has one permanent dialer and dashboard renders every modem record', async () => {
  const shell = await source('../src/components/AppShell.vue')
  const dashboard = await source('../src/views/DashboardView.vue')
  const dialer = await source('../src/components/DialerPanel.vue')
  const lineSelector = await source('../src/components/LineSelector.vue')
  const styles = await source('../src/style.css')

  const dashboardIndex = shell.indexOf("{ name: 'dashboard'")
  const contactsIndex = shell.indexOf("{ name: 'contacts'")
  const messagesIndex = shell.indexOf("{ name: 'messages'")
  const callsIndex = shell.indexOf("{ name: 'calls'")
  const recordingsIndex = shell.indexOf("{ name: 'recordings'")

  assert.ok(dashboardIndex < contactsIndex)
  assert.ok(contactsIndex < messagesIndex)
  assert.ok(messagesIndex < callsIndex)
  assert.ok(callsIndex < recordingsIndex)
  assert.match(shell, /window\.matchMedia\('\(min-width: 1480px\)'\)/)
  assert.ok(
    shell.indexOf('<IncomingCallModeControl v-if="sessionState.role === \'admin\'" />') <
      shell.indexOf('<AudioSettingsMenu />')
  )
  assert.match(
    shell,
    /<DialerPanel[\s\S]*:permanent="permanentDialer"[\s\S]*:non-modal="nonModalDialer"/
  )
  assert.doesNotMatch(shell, /dialer-fab|Grid3X3/)
  assert.match(shell, /:aria-pressed="uiState\.dialerOpen \|\| activeCallPresent"/)
  assert.doesNotMatch(shell, /<CallSurface/)
  assert.match(
    dialer,
    /v-if="permanent \|\| uiState\.dialerOpen \|\| callSurfaceVisible"/
  )
  assert.match(dialer, /<CallSurface v-if="showingCall" \/>/)
  assert.match(
    dialer,
    /@mousedown\.self="[\s\S]*!permanent && !nonModal && !showingCall && closeDialer\(\)/
  )
  assert.match(
    dialer,
    /@keydown\.esc="[\s\S]*showingCall \? minimizeCallSurface\(\) : closeDialer\(\)/
  )
  assert.match(
    dashboard,
    /const moduleLines = computed\(\(\) => displayModuleLines\(lines\.value, devicesResource\.data\)\)/
  )
  assert.match(dashboard, /v-for="line in moduleLines"/)
  assert.doesNotMatch(dashboard, /moduleLines(?:\.value)?\.slice/)
  assert.match(dialer, /<LineSelector/)
  assert.match(dialer, /:lines="lines"/)
  assert.match(dialer, /:disabled-values="unavailableDialLineIDs"/)
  assert.match(
    dialer,
    /const dialLines = computed\(\(\) =>[\s\S]*lines\.value\.filter\(lineCanPlaceVoiceCall\)/
  )
  assert.match(lineSelector, /v-for="\(option, index\) in options"/)
  assert.doesNotMatch(lineSelector, /lines(?:\.value)?\.slice/)
  assert.doesNotMatch(styles, /\.dialer-fab/)
})

test('active calls own the dialer surface and keep modal call controls reachable', async () => {
  const dialer = await source('../src/components/DialerPanel.vue')
  const surface = await source('../src/components/CallSurface.vue')

  assert.match(dialer, /ref="panelRef"/)
  assert.match(
    dialer,
    /:tabindex="!permanent && !nonModal && callSurfaceVisible \? -1 : undefined"/
  )
  assert.match(dialer, /function trapCallFocus\(event: KeyboardEvent\)/)
  assert.match(dialer, /props\.nonModal \|\|/)
  assert.match(dialer, /function restoreDialogFocus\(\): void/)
  assert.match(dialer, /dialerReturnFocus\?\.isConnected/)
  assert.match(dialer, /panelRef\.value\?\.focus\(\)/)
  assert.match(dialer, /@keydown="trapCallFocus"/)
  assert.match(dialer, /@media \(max-width: 1100px\)/)
  assert.match(surface, /role="group"\s+:aria-label="t\('calls\.controls'\)"/)
  assert.match(surface, /role="group"\s+:aria-label="t\('calls\.keypad'\)"/)
  assert.match(surface, /class="call-surface__content" :class="\{ 'is-dtmf-open': dtmfOpen \}"/)
  assert.match(surface, /@media \(prefers-reduced-motion: reduce\)/)
  assert.match(surface, /max\(20px, env\(safe-area-inset-top\)\)/)
})

test('recordings are a communication workspace with native playback and call linkage', async () => {
  const router = await source('../src/router/index.ts')
  const view = await source('../src/views/RecordingsView.vue')

  assert.match(router, /path: 'recordings',\s*name: 'recordings'/)
  assert.match(view, /import WorkspaceMasterDetail from/)
  assert.match(view, /import WorkspaceDetailPane from/)
  assert.match(view, /<WorkspaceMasterDetail/)
  assert.match(
    view,
    /<WorkspaceDetailPane[\s\S]*:content-key="initialLoading \|\| skeletonPreviewEnabled \? null : selected\?\.id"/
  )
  assert.match(
    view,
    /<audio[\s\S]*:src="selected\.download_url"[\s\S]*:volume="audioState\.recordingPlaybackVolume \/ 100"[\s\S]*controls/
  )
  assert.match(view, /:download="`modemdeck-\$\{selected\.id\}\.ogg`"/)
  assert.match(view, /:to="\{ name: 'calls', query: \{ selected: selected\.call\.id \} \}"/)
  assert.doesNotMatch(view, /marketing|hero/)
})
