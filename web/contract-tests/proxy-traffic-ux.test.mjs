import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { trafficLineState } from '../src/components/trafficLineState.ts'

const appShell = readFileSync(new URL('../src/components/AppShell.vue', import.meta.url), 'utf8')
const router = readFileSync(new URL('../src/router/index.ts', import.meta.url), 'utf8')
const trafficView = readFileSync(new URL('../src/views/TrafficView.vue', import.meta.url), 'utf8')
const trafficLineCard = readFileSync(
  new URL('../src/components/TrafficLineCard.vue', import.meta.url),
  'utf8'
)
const proxyEditor = readFileSync(
  new URL('../src/components/ProxyEditorModal.vue', import.meta.url),
  'utf8'
)
const proxyCard = readFileSync(new URL('../src/components/ProxyCard.vue', import.meta.url), 'utf8')
const runtimeEvents = readFileSync(
  new URL('../src/state/runtimeEvents.ts', import.meta.url),
  'utf8'
)
const networkState = readFileSync(
  new URL('../src/state/network.ts', import.meta.url),
  'utf8'
)

test('traffic is a primary route after recordings with exact active state', () => {
  const recordingIndex = appShell.indexOf("{ name: 'recordings'")
  const trafficIndex = appShell.indexOf("{ name: 'traffic'")
  assert.ok(recordingIndex >= 0 && trafficIndex > recordingIndex)
  assert.match(appShell, /label: t\('shell\.traffic'\), icon: ChartNoAxesCombined/)
  assert.match(appShell, /route\.name === item\.name/)
  assert.match(router, /path: 'traffic',\s*name: 'traffic'/)
  assert.match(router, /views\/TrafficView\.vue/)
})

test('traffic defaults to all lines and never selects the first module', () => {
  assert.match(trafficView, /const selectedLineID = ref\('all'\)/)
  assert.match(trafficView, /t\('traffic\.allLines'\)/)
  assert.doesNotMatch(trafficView, /selectedLineID\.value\s*=\s*lines\.value\[0\]/)
  assert.match(trafficView, /line_settings\.default_line_id/)
  assert.match(trafficView, /:initial-line-id="editorInitialLineID"/)
})

test('traffic identity uses the shared line label and module fallback', () => {
  assert.match(trafficView, /if \(line\) return lineLabel\(line\)/)
  assert.match(proxyEditor, /<LineSelector/)
  assert.doesNotMatch(proxyEditor, /<select/)
  assert.match(trafficLineCard, /return props\.line\.phone_number\.trim\(\) \|\| props\.fallback/)
  assert.match(trafficLineCard, /operator !== primaryIdentity\(\)/)
})

test('traffic line state treats inactive data as neutral and preserves explicit errors', () => {
  const disconnected = {
    line_id: 'line-main',
    connected: false,
    interface: '',
    dns: [],
    rx_bytes: 0,
    tx_bytes: 0,
    error: ''
  }

  assert.deepEqual(trafficLineState(undefined), {
    kind: 'idle',
    labelKey: 'traffic.lineDisabled'
  })
  assert.deepEqual(trafficLineState(disconnected), {
    kind: 'idle',
    labelKey: 'traffic.lineDisconnected'
  })
  assert.deepEqual(
    trafficLineState({
      ...disconnected,
      error: 'line has no connected data bearer'
    }),
    { kind: 'idle', labelKey: 'traffic.lineDisconnected' }
  )
  assert.deepEqual(
    trafficLineState({
      ...disconnected,
      connected: true
    }),
    { kind: 'connected', labelKey: 'traffic.lineConnected' }
  )
  assert.deepEqual(
    trafficLineState({
      ...disconnected,
      connected: true,
      error: 'connected bearer has no usable DNS servers'
    }),
    { kind: 'error', labelKey: 'traffic.lineError' }
  )
})

test('traffic line cards use a balanced responsive information grid', () => {
  assert.doesNotMatch(trafficLineCard, /<Cable|t\('traffic\.interface'\)/)
  assert.doesNotMatch(trafficLineCard, /v-if="connection\.kind !== 'idle'"/)
  assert.match(trafficLineCard, /<Clock3 v-else/)
  assert.match(
    trafficLineCard,
    /\.traffic-line-card dl\s*\{[\s\S]*grid-template-columns:\s*repeat\(2,\s*minmax\(0,\s*1fr\)\)/
  )
  assert.match(
    trafficLineCard,
    /@container \(max-width:\s*280px\)[\s\S]*\.traffic-line-card dl\s*\{[\s\S]*grid-template-columns:\s*minmax\(0,\s*1fr\)/
  )
  assert.match(
    trafficView,
    /\.traffic-line-grid\s*\{[\s\S]*grid-template-columns:\s*repeat\(auto-fit,\s*minmax\(320px,\s*420px\)\)[\s\S]*justify-content:\s*start/
  )
  assert.match(
    trafficView,
    /\.traffic-line-grid\s*>\s*:deep\(\.traffic-line-card\)\s*\{[\s\S]*width:\s*100%[\s\S]*max-width:\s*420px/
  )
  assert.match(
    trafficView,
    /@media \(max-width:\s*560px\)[\s\S]*\.traffic-line-grid\s*\{[\s\S]*grid-template-columns:\s*minmax\(0,\s*1fr\)/
  )
})

test('proxy facts reserve enough space for localized labels', () => {
  assert.match(
    proxyCard,
    /\.proxy-card dl > div\s*\{[\s\S]*grid-template-columns:\s*84px minmax\(0,\s*1fr\)/
  )
  assert.match(proxyCard, /\.proxy-card dt\s*\{[\s\S]*white-space:\s*nowrap/)
})

test('proxy editor exposes product fields but no interface input', () => {
  assert.match(proxyEditor, /:label="t\('proxy\.line'\)"/)
  assert.match(proxyEditor, /t\('common\.protocol'\)/)
  assert.match(proxyEditor, /t\('proxy\.listenAddress'\)/)
  assert.match(proxyEditor, /t\('common\.port'\)/)
  assert.match(proxyEditor, /t\('common\.username'\)/)
  assert.match(proxyEditor, /t\('common\.password'\)/)
  assert.doesNotMatch(proxyEditor, /v-model="form\.interface"/)
  assert.match(proxyEditor, /proxy\?\.has_password \? t\('proxy\.keepPassword'\) : ''/)
  assert.match(proxyEditor, /localError\.message \|\| props\.error/)
  assert.match(proxyEditor, /<OverlayDialog/)
  assert.match(proxyEditor, /initial-focus=/)
  assert.match(proxyEditor, /button\[aria-haspopup="listbox"\]:not\(\[disabled\]\)/)
  assert.match(proxyEditor, /isIPAddress\(form\.listen_address\)/)
  assert.match(proxyEditor, /proxyCredentialError\(/)
})

test('proxy cards keep runtime truth separate from desired apply state', () => {
  for (const key of [
    'proxy.running',
    'proxy.waitingForLine',
    'proxy.disabled',
    'proxy.configurationError',
    'proxy.runtimeUnknown'
  ]) {
    assert.ok(proxyCard.includes(`t('${key}')`), `missing ${key}`)
  }
  for (const key of ['proxy.pendingCreate', 'proxy.pendingUpdate', 'proxy.deleting']) {
    assert.ok(proxyCard.includes(`t('${key}')`), `missing ${key}`)
  }
  assert.match(proxyCard, /return props\.runtime\?\.state \|\| 'unknown'/)
  assert.doesNotMatch(proxyCard, /props\.proxy\.enabled \? 'waiting_for_bearer'/)
  assert.ok(
    [...proxyCard.matchAll(/:disabled="busy \|\| pendingDelete"/g)].length >= 3
  )
  assert.match(proxyCard, /role="switch"/)
  assert.match(trafficView, /class="proxy-add-card"/)
  assert.match(trafficView, /t\('traffic\.addProxy'\)/)
})

test('runtime SSE refreshes line inventory and guards newer proxy state', () => {
  assert.match(trafficView, /loadBootstrap\(true\)/)
  assert.doesNotMatch(trafficView, /setInterval/)
  assert.match(runtimeEvents, /gateway\.subscribeRuntimeEvents/)
  assert.match(
    runtimeEvents,
    /case 'lines':[\s\S]*?await refreshDeviceWorkspace\(\)/
  )
  assert.match(runtimeEvents, /case 'network':[\s\S]*?await loadNetwork\(true, true\)/)
  assert.match(runtimeEvents, /case 'calls':[\s\S]*?requestActiveCallRefresh\(\)/)
  assert.match(networkState, /acceptNetworkSnapshot\(snapshot, proxies\)/)
  assert.match(runtimeEvents, /if \(currentGeneration !== generation \|\| state\.connected\) return/)
  assert.match(trafficView, /:error="networkState\.error"/)
})

test('traffic reports stale and exhausted synchronization without verbose detail', () => {
  assert.match(trafficView, /t\('traffic\.runtimeStale'\)/)
  assert.match(trafficView, /t\('traffic\.syncExhausted'\)/)
  assert.match(trafficView, /current\.apply_pending/)
  assert.match(trafficView, /current\.apply_exhausted/)
})
