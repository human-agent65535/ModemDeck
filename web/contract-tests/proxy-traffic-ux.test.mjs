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

test('traffic is a primary route after recordings with exact active state', () => {
  const recordingIndex = appShell.indexOf("{ name: 'recordings'")
  const trafficIndex = appShell.indexOf("{ name: 'traffic'")
  assert.ok(recordingIndex >= 0 && trafficIndex > recordingIndex)
  assert.match(appShell, /label: '流量', icon: ChartNoAxesCombined/)
  assert.match(appShell, /route\.name === item\.name/)
  assert.match(router, /path: 'traffic',\s*name: 'traffic'/)
  assert.match(router, /views\/TrafficView\.vue/)
})

test('traffic defaults to all lines and never selects the first module', () => {
  assert.match(trafficView, /const selectedLineID = ref\('all'\)/)
  assert.match(trafficView, />\s*全部线路\s*</)
  assert.doesNotMatch(trafficView, /selectedLineID\.value\s*=\s*lines\.value\[0\]/)
  assert.match(trafficView, /line_settings\.default_device_imei/)
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
    label: '未启用'
  })
  assert.deepEqual(trafficLineState(disconnected), {
    kind: 'idle',
    label: '未连接'
  })
  assert.deepEqual(
    trafficLineState({
      ...disconnected,
      error: 'line has no connected data bearer'
    }),
    { kind: 'idle', label: '未连接' }
  )
  assert.deepEqual(
    trafficLineState({
      ...disconnected,
      connected: true
    }),
    { kind: 'connected', label: '已联网' }
  )
  assert.deepEqual(
    trafficLineState({
      ...disconnected,
      connected: true,
      error: 'connected bearer has no usable DNS servers'
    }),
    { kind: 'error', label: '状态异常' }
  )
})

test('traffic line cards use the device-card responsive width contract', () => {
  assert.match(
    trafficView,
    /\.traffic-line-grid\s*\{[\s\S]*grid-template-columns:\s*repeat\(auto-fit,\s*minmax\(320px,\s*1fr\)\)[\s\S]*justify-content:\s*start/
  )
  assert.match(
    trafficView,
    /\.traffic-line-grid\s*>\s*:deep\(\.traffic-line-card\)\s*\{[\s\S]*width:\s*100%[\s\S]*max-width:\s*420px/
  )
  assert.match(
    trafficView,
    /@media \(max-width:\s*600px\)[\s\S]*\.traffic-line-grid\s*\{[\s\S]*grid-template-columns:\s*minmax\(0,\s*1fr\)/
  )
})

test('proxy editor exposes product fields but no interface input', () => {
  assert.match(proxyEditor, /label="线路"/)
  assert.match(proxyEditor, />协议</)
  assert.match(proxyEditor, />监听地址</)
  assert.match(proxyEditor, />端口</)
  assert.match(proxyEditor, />用户名</)
  assert.match(proxyEditor, />密码</)
  assert.doesNotMatch(proxyEditor, /v-model="form\.interface"/)
  assert.doesNotMatch(proxyEditor, />网卡</)
  assert.match(proxyEditor, /proxy\?\.has_password \? '留空则保留'/)
  assert.match(proxyEditor, /localError\.message \|\| props\.error/)
  assert.match(proxyEditor, /event\.key === 'Escape'/)
  assert.match(proxyEditor, /event\.key !== 'Tab'/)
  assert.match(proxyEditor, /button\[aria-haspopup="listbox"\]:not\(\[disabled\]\)/)
  assert.match(proxyEditor, /isIPAddress\(form\.listen_address\)/)
  assert.match(proxyEditor, /proxyCredentialError\(/)
})

test('proxy cards keep runtime truth separate from desired apply state', () => {
  for (const label of ['运行中', '等待线路联网', '已停用', '配置错误', '运行态未知']) {
    assert.match(proxyCard, new RegExp(label))
  }
  for (const label of ['等待创建', '等待同步', '正在删除']) {
    assert.match(proxyCard, new RegExp(label))
  }
  assert.match(proxyCard, /return props\.runtime\?\.state \|\| 'unknown'/)
  assert.doesNotMatch(proxyCard, /props\.proxy\.enabled \? 'waiting_for_bearer'/)
  assert.ok(
    [...proxyCard.matchAll(/:disabled="busy \|\| pendingDelete"/g)].length >= 3
  )
  assert.match(proxyCard, /role="switch"/)
  assert.match(trafficView, /class="proxy-add-card"/)
  assert.match(trafficView, />\s*添加代理\s*</)
})

test('traffic refreshes line inventory and guards newer proxy state', () => {
  assert.match(trafficView, /loadBootstrap\(true\)/)
  assert.match(trafficView, /Promise\.all\(\[loadBootstrap\(true\), loadNetwork\(true, true\)\]\)/)
  assert.match(trafficView, /:error="networkState\.error"/)
})

test('traffic reports stale and exhausted synchronization without verbose detail', () => {
  assert.match(trafficView, /运行状态暂未更新/)
  assert.match(trafficView, /代理配置同步失败，已停止重试/)
  assert.match(trafficView, /current\.apply_pending/)
  assert.match(trafficView, /current\.apply_exhausted/)
})
