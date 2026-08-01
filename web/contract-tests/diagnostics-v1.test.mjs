import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const source = await readFile(
  new URL('../src/components/DiagnosticsPanel.vue', import.meta.url),
  'utf8'
)
const client = await readFile(
  new URL('../src/api/client.ts', import.meta.url),
  'utf8'
)

test('Diagnostics V1 is organized around evidence, recovery, and logs', () => {
  const runtime = source.indexOf("t('diagnostics.runtimeStatus')")
  const evidence = source.indexOf("t('diagnostics.deviceEvidence')")
  const recovery = source.indexOf("t('device.faultRecovery')")
  const logs = source.indexOf("t('diagnostics.runtimeLogs')")

  assert.ok(runtime >= 0)
  assert.ok(evidence > runtime)
  assert.ok(recovery > evidence)
  assert.ok(logs > recovery)
  assert.doesNotMatch(source, /function lineCapabilities|function agentCapabilities/)
})

test('call session diagnostics remain visible while the runtime is idle', () => {
  assert.doesNotMatch(
    source,
    /<section v-if="snapshot\.active_calls\.length" class="diagnostics-section">/
  )
  assert.match(
    source,
    /<div v-if="snapshot\.active_calls\.length" class="active-call-list">/
  )
  assert.match(source, /v-else class="empty-row"[^>]*>\{\{ t\('diagnostics\.noActiveCalls'\) \}\}/)
})

test('line evidence exposes diagnostic-only identities and raw state', () => {
  assert.match(source, /selectedDiagnosticLine\.endpoint_id/)
  assert.match(source, /lineFailureEvidence\(selectedDiagnosticLine\)/)
  assert.match(source, /lineRegistrationEvidence\(selectedDiagnosticLine\)/)
  assert.match(source, /lineAccessMask\(selectedDiagnosticLine\)/)
  assert.match(source, /selectedDiagnosticHardware\.radio\.power_state_code/)
  assert.match(source, /selectedDiagnosticHardware\.voice_verification/)
  assert.doesNotMatch(source, /selectedDiagnosticLine\.device_imei/)
  assert.doesNotMatch(source, /selectedDiagnosticLine\.iccid/)
})

test('failed lines are summarized together without replacing per-line recovery', () => {
  assert.match(source, /failedLines\.value\.length === diagnosticLines\.value\.length/)
  assert.match(source, /failedLines\.length > 0/)
  assert.match(source, /allDiagnosticLinesFailed/)
  assert.match(source, /v-for="line in failedLines"/)
  assert.match(source, /failure_reason_code/)
  assert.match(source, /resetDiagnosticUSBDevice\(line\.id\)/)
})

test('device capability evidence is complete and preserves raw backend flags', () => {
  for (const capability of [
    'voice',
    'flight_mode',
    'vowifi',
    'volte',
    'esim',
    'ussd',
    'connection_profile',
    'radio',
    'data_connection',
    'at_terminal',
    'usb_reset'
  ]) {
    assert.match(source, new RegExp(`capabilities\\.${capability}`))
  }
  assert.match(source, /item\.capability\.backend/)
  assert.match(source, /supported=\$\{capability\.supported\}/)
  assert.match(source, /implemented=\$\{capability\.implemented\}/)
  assert.match(source, /readable=\$\{capability\.readable\}/)
  assert.match(source, /writable=\$\{capability\.writable\}/)
})

test('USB recovery loads device capabilities and requires confirmation', () => {
  assert.match(source, /loadDiagnosticDeviceConfiguration\(lineID\)/)
  assert.match(source, /capabilities\.usb_reset/)
  assert.match(source, /requestConfirmation\(\{/)
  assert.match(source, /tone:\s*'danger'/)
  assert.match(source, /resetDiagnosticUSBDevice\(line\.id\)/)
})

test('snapshot failure stays local while recovery and logs remain available', () => {
  assert.match(source, /<SettingsLoadBoundary[\s\S]*:loading="initialLoading"/)
  assert.doesNotMatch(source, /:error="snapshotState === 'error'"/)
  assert.doesNotMatch(source, /:forbidden="snapshotState === 'forbidden'"/)
  assert.match(source, /v-if="!snapshot" class="runtime-errors"/)
  assert.match(source, /bootstrapResource\.data\?\.line_catalog/)
  assert.match(source, /loadBootstrap\(\)/)
  assert.match(source, /recoveryLineIDs\.value\.map\(lineID =>/)
  assert.match(client, /get\('diagnosticsFixture'\) === 'error'/)

  const recovery = source.indexOf("t('device.faultRecovery')")
  const logs = source.indexOf("t('diagnostics.runtimeLogs')")
  assert.ok(recovery >= 0)
  assert.ok(logs > recovery)
  assert.match(
    client,
    /const lines = source\.lines === null \? \[\] : source\.lines/
  )
  assert.match(
    client,
    /const activeCalls = source\.active_calls === null \? \[\] : source\.active_calls/
  )
})

test('runtime log connection colors describe reachable stream states', () => {
  assert.match(
    source,
    /\.overall-status\.is-ok,\s*\.connection-state\.is-live\s*\{\s*color: var\(--success\)/
  )
  assert.match(
    source,
    /\.connection-state\.is-reconnecting,\s*\.connection-state\.is-connecting\s*\{\s*color: var\(--warning\)/
  )
  assert.match(
    source,
    /\.connection-state\.is-paused\s*\{\s*color: var\(--muted\)/
  )
  assert.match(
    source,
    /\.connection-state > span\s*\{[^}]*color: inherit;[^}]*background: currentColor;/s
  )
  assert.doesNotMatch(source, /connectionState\.value = 'error'/)
  assert.doesNotMatch(source, /case 'error'/)
})
