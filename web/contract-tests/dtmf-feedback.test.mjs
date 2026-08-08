import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

import { dtmfFrequencies } from '../src/state/dtmfAudio.ts'

const source = path => readFile(new URL(path, import.meta.url), 'utf8')

test('DTMF feedback uses the standard dual-tone frequency grid', () => {
  assert.deepEqual(dtmfFrequencies('1'), [697, 1209])
  assert.deepEqual(dtmfFrequencies('5'), [770, 1336])
  assert.deepEqual(dtmfFrequencies('9'), [852, 1477])
  assert.deepEqual(dtmfFrequencies('*'), [941, 1209])
  assert.deepEqual(dtmfFrequencies('0'), [941, 1336])
  assert.deepEqual(dtmfFrequencies('#'), [941, 1477])
  assert.equal(dtmfFrequencies('+'), undefined)
})

test('DTMF feedback creates two short browser-audio oscillators per key press', async () => {
  const audio = await source('../src/state/dtmfAudio.ts')

  assert.match(audio, /new AudioContext\(\)/)
  assert.match(audio, /for \(const frequency of pair\)/)
  assert.match(audio, /audioContext\.createOscillator\(\)/)
  assert.match(audio, /const stop = start \+ 0\.12/)
  assert.match(audio, /oscillator\.stop\(stop\)/)
  assert.match(audio, /oscillator\.disconnect\(\)/)
  assert.match(audio, /if \(remainingOscillators === 0\) gain\.disconnect\(\)/)
})

test('dial and in-call keypads share visual data, sound, and pressed-digit feedback', async () => {
  const dialer = await source('../src/components/DialerPanel.vue')
  const call = await source('../src/components/CallSurface.vue')
  const styles = await source('../src/style.css')

  assert.match(dialer, /v-for="key in phoneKeypad"/)
  assert.match(call, /v-for="key in phoneKeypad"/)
  assert.match(dialer, /playDTMFTone\(digit\)/)
  assert.match(call, /playDTMFTone\(digit\)/)
  assert.match(call, /const dtmfDigits = computed\(\(\) => callState\.dtmfDigits\)/)
  assert.match(call, /class="call-surface__dtmf-display"/)
  assert.match(styles, /\.keypad__key:active:not\(:disabled\)/)
})

test('in-call keypad digits survive keypad and call-surface minimization', async () => {
  const callState = await source('../src/state/call.ts')
  const call = await source('../src/components/CallSurface.vue')

  assert.match(callState, /dtmfDigits: string/)
  assert.match(callState, /callState\.dtmfDigits \+= digit/)
  assert.match(callState, /if \(newCall\) \{[\s\S]*callState\.dtmfDigits = ''/)
  assert.match(call, /const dtmfDigits = computed\(\(\) => callState\.dtmfDigits\)/)
  assert.doesNotMatch(call, /const dtmfDigits = ref/)
})

test('pre-call delete stays in the number field and the call action owns a footer', async () => {
  const [dialer, styles] = await Promise.all([
    source('../src/components/DialerPanel.vue'),
    source('../src/style.css')
  ])

  assert.match(
    dialer,
    /class="dialer-number-control"[\s\S]*class="icon-button dialer-backspace-button"/
  )
  assert.match(
    dialer,
    /class="dialer-primary-actions keypad-action-grid"[\s\S]*class="call-button"/
  )
  assert.match(styles, /\.keypad-action-grid \{[\s\S]*border-top: 1px solid var\(--border\)/)
  assert.match(dialer, /\.dialer-number-trailing \{[\s\S]*position: absolute/)
  assert.match(dialer, /\.dialer-number-trailing \.dialer-backspace-button \{/)
  assert.doesNotMatch(dialer, /class="dialer-actions"/)
})

test('per-call recording is a footer action and does not crowd the search area', async () => {
  const dialer = await source('../src/components/DialerPanel.vue')
  const call = await source('../src/components/CallSurface.vue')

  assert.match(
    dialer,
    /class="dialer-primary-actions keypad-action-grid"[\s\S]*class="dialer-recording-action keypad-action-item"[\s\S]*class="dialer-primary-action keypad-action-item"/
  )
  assert.match(dialer, /:aria-pressed="dialerRecordingState\.enabled"/)
  assert.doesNotMatch(dialer, /<label class="dialer-recording">/)
  assert.doesNotMatch(dialer, /dialer-recording-action__state/)
  assert.doesNotMatch(call, /call-footer-action__state|recording-choice|is-changing/)
  assert.match(call, /:aria-pressed="callRecordingState\.enabled"/)
  assert.match(
    dialer,
    /\.dialer-recording-action:not\(\.is-active\):hover:not\(:disabled\)/
  )
  assert.match(
    call,
    /\.call-footer-action:not\(\.is-active\):hover:not\(:disabled\)/
  )
})

test('dial and in-call keypads share a bottom-aligned interaction stage', async () => {
  const [dialer, call, styles] = await Promise.all([
    source('../src/components/DialerPanel.vue'),
    source('../src/components/CallSurface.vue'),
    source('../src/style.css')
  ])

  assert.match(dialer, /class="dialer-keypad-stage"[\s\S]*class="keypad"/)
  assert.match(
    dialer,
    /\.dialer-keypad-stage \{[\s\S]*flex: 1 0 311px;[\s\S]*align-items: flex-end;/
  )
  assert.match(
    styles,
    /\.keypad-action-grid \{[\s\S]*min-height: 108px;[\s\S]*grid-template-columns: repeat\(3, var\(--keypad-track-size\)\);/
  )
  assert.match(
    styles,
    /\.keypad-action-item \{[\s\S]*grid-template-rows: var\(--keypad-action-primary-size\) 14px;/
  )
  assert.match(
    call,
    /\.call-surface__content\.is-dtmf-open \.call-surface__dtmf \{[\s\S]*margin-top: auto;/
  )
  assert.match(
    dialer,
    /class="dialer-primary-actions keypad-action-grid"[\s\S]*class="dialer-recording-action keypad-action-item"[\s\S]*class="dialer-primary-action keypad-action-item"/
  )
  assert.match(
    call,
    /class="call-surface__primary-actions keypad-action-grid"[\s\S]*class="call-footer-action call-footer-action--recording keypad-action-item"[\s\S]*class="call-primary-action call-primary-action--center keypad-action-item"[\s\S]*class="call-footer-action call-footer-action--keypad keypad-action-item"/
  )
  assert.doesNotMatch(
    dialer,
    /\.dialer-recording-action:not\(\.is-active\):hover:not\(:disabled\)[^{]*\{[^}]*transform:/
  )
  assert.doesNotMatch(
    call,
    /\.call-footer-action:not\(\.is-active\):hover:not\(:disabled\)[^{]*\{[^}]*transform:/
  )
})
