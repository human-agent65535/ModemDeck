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

test('app startup requests microphone access once and releases the probe stream', async () => {
  const app = await source('../src/App.vue')
  const audio = await source('../src/state/audio.ts')
  const initialize = section(
    audio,
    'export function initializeBrowserAudio(): Promise<void> {',
    'function onDeviceChange(): void {'
  )

  assert.match(app, /onMounted\(\(\) => \{\s*void initializeBrowserAudio\(\)/)
  assert.match(app, /onBeforeUnmount\(\(\) => \{\s*shutdownAudioDevices\(\)/)
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
    /connection\.connectionState === 'disconnected'\) \{\s*failConnection\(/
  )
  assert.doesNotMatch(
    callMedia,
    /connection\.connectionState === 'disconnected'\) \{\s*callMediaState\.status = 'connecting'/
  )
})

test('desktop shell has one permanent dialer and dashboard renders every line', async () => {
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
  assert.match(shell, /window\.matchMedia\('\(min-width: 1101px\)'\)/)
  assert.ok(shell.indexOf('<IncomingCallModeControl />') < shell.indexOf('<AudioSettingsMenu />'))
  assert.match(shell, /<DialerPanel :permanent="permanentDialer" \/>/)
  assert.doesNotMatch(shell, /dialer-fab|Grid3X3/)
  assert.match(shell, /:aria-pressed="uiState\.dialerOpen"/)
  assert.doesNotMatch(shell, /<CallSurface/)
  assert.match(dialer, /v-if="permanent \|\| uiState\.dialerOpen \|\| showingCall"/)
  assert.match(dialer, /<CallSurface v-if="showingCall" \/>/)
  assert.match(
    dialer,
    /@mousedown\.self="!permanent && !showingCall && closeDialer\(\)"/
  )
  assert.match(
    dialer,
    /@keydown\.esc="!permanent && !showingCall && closeDialer\(\)"/
  )
  assert.match(dashboard, /v-for="line in lines"/)
  assert.doesNotMatch(dashboard, /lines(?:\.value)?\.slice/)
  assert.match(dialer, /<LineSelector/)
  assert.match(dialer, /:lines="lines"/)
  assert.match(lineSelector, /v-for="\(option, index\) in options"/)
  assert.doesNotMatch(lineSelector, /lines(?:\.value)?\.slice/)
  assert.doesNotMatch(styles, /\.dialer-fab/)
})

test('active calls own the dialer surface and keep modal call controls reachable', async () => {
  const dialer = await source('../src/components/DialerPanel.vue')
  const surface = await source('../src/components/CallSurface.vue')

  assert.match(dialer, /ref="panelRef"/)
  assert.match(dialer, /:tabindex="!permanent && showingCall \? -1 : undefined"/)
  assert.match(dialer, /function trapCallFocus\(event: KeyboardEvent\)/)
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
  assert.match(view, /class="workspace"/)
  assert.match(view, /class="list-pane"/)
  assert.match(view, /class="detail-pane"/)
  assert.match(
    view,
    /<audio[\s\S]*:src="selected\.download_url"[\s\S]*:volume="audioState\.recordingPlaybackVolume \/ 100"[\s\S]*controls/
  )
  assert.match(view, /:download="`modemdeck-\$\{selected\.id\}\.ogg`"/)
  assert.match(view, /:to="\{ name: 'calls', query: \{ selected: selected\.call\.id \} \}"/)
  assert.doesNotMatch(view, /marketing|hero/)
})
