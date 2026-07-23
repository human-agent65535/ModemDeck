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

test('opening audio settings enumerates devices without requesting the microphone', async () => {
  const menu = await source('../src/components/AudioSettingsMenu.vue')
  const audio = await source('../src/state/audio.ts')
  const openMenu = section(
    menu,
    'async function openDialog(): Promise<void> {',
    'function toggle(): void {'
  )
  const enumerate = section(
    audio,
    'export function refreshAudioDevices(): Promise<void> {',
    'function onDeviceChange(): void {'
  )

  assert.match(openMenu, /refreshAudioDevices\(\)/)
  assert.doesNotMatch(openMenu, /startMicrophoneTest|getUserMedia/)
  assert.match(enumerate, /enumerateDevices\(\)/)
  assert.doesNotMatch(enumerate, /getUserMedia/)
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
    'for (const track of stream.getTracks()) track.stop()'
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
  assert.match(shell, /<IncomingCallModeControl \/>\s*<AudioSettingsMenu \/>/)
  assert.match(shell, /<DialerPanel :permanent="permanentDialer" \/>/)
  assert.doesNotMatch(shell, /dialer-fab|Grid3X3|dialerOpen/)
  assert.match(dialer, /v-if="permanent \|\| uiState\.dialerOpen"/)
  assert.match(dashboard, /v-for="line in lines"/)
  assert.doesNotMatch(dashboard, /lines(?:\.value)?\.slice/)
  assert.match(dialer, /<LineSelector/)
  assert.match(dialer, /:lines="lines"/)
  assert.match(lineSelector, /v-for="line in lines"/)
  assert.doesNotMatch(lineSelector, /lines(?:\.value)?\.slice/)
  assert.doesNotMatch(styles, /\.dialer-fab/)
})

test('recordings are a communication workspace with native playback and call linkage', async () => {
  const router = await source('../src/router/index.ts')
  const view = await source('../src/views/RecordingsView.vue')

  assert.match(router, /path: 'recordings',\s*name: 'recordings'/)
  assert.match(view, /class="workspace"/)
  assert.match(view, /class="list-pane"/)
  assert.match(view, /class="detail-pane"/)
  assert.match(view, /<audio :src="selected\.download_url" controls preload="metadata">/)
  assert.match(view, /:download="`modemdeck-\$\{selected\.id\}\.ogg`"/)
  assert.match(view, /:to="\{ name: 'calls', query: \{ selected: selected\.call\.id \} \}"/)
  assert.doesNotMatch(view, /marketing|hero/)
})
