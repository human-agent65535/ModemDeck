import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const [dashboard, devicePanel, moduleCard] = await Promise.all([
  readFile(new URL('../src/views/DashboardView.vue', import.meta.url), 'utf8'),
  readFile(
    new URL('../src/components/DeviceConfigurationPanel.vue', import.meta.url),
    'utf8'
  ),
  readFile(new URL('../src/components/ModuleCard.vue', import.meta.url), 'utf8')
])

function cssBlock(source, selector) {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  return source.match(new RegExp(`${escaped}\\s*\\{([^}]*)\\}`))?.[1] || ''
}

test('dashboard and device settings use the same stable module-card tracks', () => {
  const dashboardGrid = cssBlock(
    dashboard,
    '.dashboard-module-grid,\n.dashboard-detail-list'
  )
  const settingsGrid = cssBlock(devicePanel, '.module-grid')

  for (const block of [dashboardGrid, settingsGrid]) {
    assert.match(
      block,
      /grid-template-columns: repeat\(auto-fill, minmax\(320px, 420px\)\)/
    )
    assert.match(block, /justify-content: start/)
    assert.match(block, /gap: 12px/)
  }

  const settingsCard = cssBlock(
    devicePanel,
    '.module-grid > :deep(.module-card)'
  )
  const settingsNarrow = devicePanel.slice(
    devicePanel.indexOf('@media (max-width: 720px)')
  )
  assert.match(settingsCard, /width: 100%/)
  assert.match(settingsCard, /max-width: 420px/)
  assert.doesNotMatch(settingsGrid, /grid-auto-flow|grid-auto-columns|overflow-x/)
  assert.doesNotMatch(devicePanel, /scroll-snap|module-grid::-webkit-scrollbar/)
  assert.match(
    settingsNarrow,
    /\.module-grid\s*\{[^}]*grid-template-columns: minmax\(0, 1fr\)/s
  )
  assert.match(
    settingsNarrow,
    /\.module-grid > :deep\(\.module-card\)\s*\{[^}]*max-width: none/s
  )
})

test('module cards expose one fixed information skeleton in both views', () => {
  const labels = [
    "t('lines.signal')",
    "t('lines.model')",
    "t('lines.firmware')",
    'IMEI',
    'ICCID',
    "t('lines.port')"
  ]
  const positions = labels.map(label => moduleCard.indexOf(label))

  assert.ok(positions.every(position => position >= 0))
  assert.deepEqual(positions, positions.slice().sort((left, right) => left - right))
  assert.match(moduleCard, /v-for="fact in networkFacts"/)
  assert.match(moduleCard, /<dt>\{\{ fact\.label \}\}<\/dt>/)
  assert.match(
    cssBlock(moduleCard, '.module-card__facts'),
    /grid-template-columns: repeat\(2, minmax\(0, 1fr\)\)/
  )
  assert.doesNotMatch(moduleCard, /compact\??:|is-compact|v-if="!compact"/)
  assert.doesNotMatch(moduleCard, /@container \(min-width: 410px\)/)
  assert.doesNotMatch(moduleCard, /repeat\(3, minmax\(0, 1fr\)\)/)
})

test('device settings add selection actions without changing card content', () => {
  const dashboardUse =
    dashboard.match(/<ModuleCard[\s\S]*?\/>/)?.[0] || ''
  const settingsUse =
    devicePanel.match(/<ModuleCard[\s\S]*?\/>/)?.[0] || ''

  for (const usage of [dashboardUse, settingsUse]) {
    assert.match(usage, /:line="line"/)
    assert.match(usage, /:device="deviceFor\(line\)"/)
    assert.match(usage, /:default-line="lineKey\(line\) === defaultLineID"/)
    assert.doesNotMatch(usage, /compact/)
  }

  assert.doesNotMatch(dashboardUse, /:selected=|\sactions(?:\s|>)/)
  assert.match(settingsUse, /:selected="line\.id === selectedLineID"/)
  assert.match(settingsUse, /\sactions/)
  assert.match(settingsUse, /@select="selectLine\(line\)"/)
  assert.match(settingsUse, /@make-default="makeDefault\(line\)"/)
})

test('voice capability describes call control without implying an audio path', () => {
  const capabilitiesStart = moduleCard.indexOf(
    '<div class="module-card__capabilities"'
  )
  const capabilitiesEnd = moduleCard.indexOf('</div>', capabilitiesStart)
  const capabilities = moduleCard.slice(capabilitiesStart, capabilitiesEnd)

  assert.match(
    capabilities,
    /:class="\{ 'is-enabled': line\.capabilities\?\.voice \}"[\s\S]*<Phone :size="14" \/>[\s\S]*t\('lines\.callControl'\)/
  )
  assert.doesNotMatch(capabilities, /browserAudio|mediaBridge|USB/)
})

test('module cards use graded bars and reserve the airplane icon for flight mode', () => {
  const flightModeStart = moduleCard.indexOf('const flightMode = computed(')
  const flightModeEnd = moduleCard.indexOf(
    'const radioWaitingForRegistration = computed(',
    flightModeStart
  )
  const flightModeProjection = moduleCard.slice(flightModeStart, flightModeEnd)

  assert.match(moduleCard, /import SignalBars from '\.\/SignalBars\.vue'/)
  assert.match(
    flightModeProjection,
    /props\.line\.radio_desired_enabled_known[\s\S]*!props\.line\.radio_desired_enabled/
  )
  assert.doesNotMatch(flightModeProjection, /state|disabled/)
  assert.match(
    moduleCard,
    /!flightMode\.value[\s\S]*!radioWaitingForRegistration\.value[\s\S]*isRegisteredNetwork\(props\.line\)/
  )
  assert.match(moduleCard, /if \(flightMode\.value\) return t\('device\.flightMode'\)/)
  assert.match(
    moduleCard,
    /if \(radioWaitingForRegistration\.value\) return t\('device\.radioRecovering'\)/
  )
  assert.match(
    moduleCard,
    /class="module-card__signal-value"[\s\S]*<SignalBars :value="signal" :flight-mode="flightMode" \/>[\s\S]*flightMode[\s\S]*t\('device\.flightMode'\)[\s\S]*signal === null[\s\S]*`\$\{signal\}%`/
  )
  assert.doesNotMatch(moduleCard, /SignalHigh|SignalMedium|SignalLow|SignalZero/)
})
