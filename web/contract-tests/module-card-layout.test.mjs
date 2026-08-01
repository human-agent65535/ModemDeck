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

test('dashboard keeps stable module-card tracks', () => {
  const dashboardGrid = cssBlock(
    dashboard,
    '.dashboard-module-grid,\n.dashboard-detail-list'
  )

  assert.match(
    dashboardGrid,
    /grid-template-columns: repeat\(auto-fill, minmax\(320px, 420px\)\)/
  )
  assert.match(dashboardGrid, /justify-content: start/)
  assert.match(dashboardGrid, /gap: 12px/)
})

test('device settings retain card selection in their dedicated drilldown workbench', () => {
  assert.match(devicePanel, /<DeviceWorkspace/)
  assert.match(devicePanel, /#selector/)
  assert.doesNotMatch(devicePanel, /SettingsMasterDetail/)
  assert.match(devicePanel, /class="module-grid"/)
  assert.match(devicePanel, /<ModuleCard/)
  assert.match(
    cssBlock(devicePanel, '.module-grid'),
    /grid-template-columns: repeat\(auto-fit, minmax\(min\(320px, 100%\), 1fr\)\)/
  )
  assert.match(cssBlock(devicePanel, '.module-grid'), /width: min\(100%, 852px\)/)
  assert.match(cssBlock(devicePanel, '.module-grid'), /align-items: stretch/)
  assert.match(
    cssBlock(devicePanel, '.module-grid > :deep(.module-card)'),
    /height: auto/
  )
})

test('device detail content stays aligned with its full-width tab rail', () => {
  const body = cssBlock(devicePanel, '.device-configuration__body')

  assert.match(body, /width: 100%/)
  assert.doesNotMatch(body, /1280px|margin-inline: auto/)
})

test('module cards expose one fixed information skeleton in both views', () => {
  const labels = [
    "t('lines.signal')",
    "t('device.accessTechnology')",
    "t('lines.model')",
    '<dt>IMEI</dt>'
  ]
  const positions = labels.map(label => moduleCard.indexOf(label))

  assert.ok(positions.every(position => position >= 0))
  assert.deepEqual(positions, positions.slice().sort((left, right) => left - right))
  assert.match(moduleCard, /v-for="fact in networkFacts"/)
  assert.match(moduleCard, /class="module-card__operator-fact"/)
  assert.match(moduleCard, /<dt>\{\{ fact\.label \}\}<\/dt>/)
  const factGrid = cssBlock(moduleCard, '.module-card__facts')
  assert.match(factGrid, /grid-template-columns: repeat\(2, minmax\(0, 1fr\)\)/)
  assert.match(factGrid, /grid-template-rows: minmax\(35px, auto\) auto auto/)
  assert.match(
    cssBlock(moduleCard, '.module-card__operator-fact'),
    /grid-row: 1/
  )
  assert.match(moduleCard, /\.filter\(fact => fact\.value !== '—'\)/)
  assert.doesNotMatch(moduleCard, /compact\??:|is-compact|v-if="!compact"/)
  assert.doesNotMatch(moduleCard, /@container \(min-width: 410px\)/)
  assert.doesNotMatch(moduleCard, /repeat\(3, minmax\(0, 1fr\)\)/)
  assert.doesNotMatch(
    moduleCard,
    /lines\.firmware|device\.lastSeen|lines\.port|ICCID/
  )
  assert.match(moduleCard, /import SensitiveValue from '\.\/SensitiveValue\.vue'/)
  assert.match(
    moduleCard,
    /<SensitiveValue[\s\S]*:value="equipmentIdentifier"[\s\S]*label="IMEI"/
  )
  assert.match(moduleCard, /props\.line\.device_imei \|\| props\.device\?\.imei/)
  assert.doesNotMatch(moduleCard, /currentBand|servingBand|serving_radio/)
  assert.match(moduleCard, /accessTechnologyLabel\(props\.line\.access_technologies\)/)
})

test('module cards stretch their body while keeping every footer fixed to the bottom', () => {
  const card = cssBlock(moduleCard, '.module-card')
  const main = cssBlock(moduleCard, '.module-card__main')
  const footer = cssBlock(moduleCard, '.module-card__footer')

  assert.match(card, /display: flex/)
  assert.match(card, /height: 100%/)
  assert.match(card, /flex-direction: column/)
  assert.match(main, /flex: 1 1 auto/)
  assert.match(main, /align-content: start/)
  assert.match(footer, /min-height: 48px/)
  assert.match(footer, /flex: 0 0 48px/)
})

test('dashboard module cards retain their canonical data projection', () => {
  const dashboardUse =
    dashboard.match(/<ModuleCard[\s\S]*?\/>/)?.[0] || ''

  assert.match(dashboardUse, /:line="line"/)
  assert.match(dashboardUse, /:device="deviceFor\(line\)"/)
  assert.match(dashboardUse, /:default-line="lineKey\(line\) === defaultLineID"/)
  assert.doesNotMatch(dashboardUse, /compact/)
  assert.doesNotMatch(dashboardUse, /:selected=|\sactions(?:\s|>)/)
})

test('module cards separate call control from verified voice media', () => {
  const capabilitiesStart = moduleCard.indexOf(
    '<div class="module-card__capabilities"'
  )
  const capabilitiesEnd = moduleCard.indexOf('</div>', capabilitiesStart)
  const capabilities = moduleCard.slice(capabilitiesStart, capabilitiesEnd)

  assert.match(
    capabilities,
    /:class="\{ 'is-enabled': lineHasCallControl\(line\) \}"[\s\S]*<Phone :size="14" \/>[\s\S]*t\('lines\.callControl'\)/
  )
  assert.match(
    capabilities,
    /v-if="line\.capabilities\?\.media === true"[\s\S]*class="is-enabled"[\s\S]*<AudioLines :size="14" \/>[\s\S]*t\('lines\.voiceCalling'\)/
  )
  assert.doesNotMatch(capabilities, /browserAudio|mediaBridge|USB/)
})

test('module card footers become accessible icon-only controls on mobile', () => {
  const mobile = moduleCard.slice(moduleCard.indexOf('@media (max-width: 860px)'))

  assert.match(
    mobile,
    /\.module-card__capabilities > span\s*\{[^}]*width: 30px[^}]*height: 30px[^}]*font-size: 0/s
  )
  assert.match(
    mobile,
    /\.module-card__default-action,[\s\S]*\.module-card__default-status\s*\{[^}]*width: 34px[^}]*font-size: 0/s
  )
  assert.match(
    moduleCard,
    /:title="t\('lines\.callControl'\)"[\s\S]*:aria-label="t\('lines\.callControl'\)"/
  )
  assert.match(
    moduleCard,
    /class="module-card__default-status"[\s\S]*:aria-label="t\('lines\.currentDefaultLine'\)"/
  )
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
