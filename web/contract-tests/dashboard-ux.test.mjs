import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const dashboard = await readFile(
  new URL('../src/views/DashboardView.vue', import.meta.url),
  'utf8'
)

test('dashboard orders communication status, lines, traffic, and favorites', () => {
  const summaryIndex = dashboard.indexOf('class="dashboard-summary-grid"')
  const linesIndex = dashboard.indexOf('id="dashboard-lines-title"')
  const trafficIndex = dashboard.indexOf('id="dashboard-traffic-title"')
  const favoritesIndex = dashboard.indexOf('id="dashboard-contacts-title"')

  assert.ok(summaryIndex >= 0)
  assert.ok(linesIndex > summaryIndex)
  assert.ok(trafficIndex > linesIndex)
  assert.ok(favoritesIndex > trafficIndex)
})

test('dashboard uses existing network data for a traffic summary and entry point', () => {
  assert.match(dashboard, /import \{ loadNetwork, networkState \} from '\.\.\/state\/network'/)
  assert.match(dashboard, /loadNetwork\(\)/)
  assert.match(dashboard, /<TrafficSummary/)
  assert.match(dashboard, /:today-bytes="todayTraffic"/)
  assert.match(dashboard, /:month-bytes="monthTraffic"/)
  assert.match(dashboard, /:to="\{ name: 'traffic' \}"/)
})

test('dashboard module cards keep the settings card width instead of stretching', () => {
  const moduleGridBlock =
    dashboard.match(/\.dashboard-module-grid\s*\{([^}]*)\}/)?.[1] || ''
  const moduleCardBlock =
    dashboard.match(
      /\.dashboard-module-grid > :deep\(\.module-card\)\s*\{([^}]*)\}/
    )?.[1] || ''

  assert.match(
    moduleGridBlock,
    /grid-template-columns: repeat\(auto-fit, minmax\(320px, 1fr\)\)/
  )
  assert.match(moduleGridBlock, /justify-content: start/)
  assert.match(moduleCardBlock, /max-width: 420px/)
})

test('dashboard favorites never substitute recent or alphabetic contacts', () => {
  assert.match(dashboard, /const favoriteContacts = computed/)
  assert.match(dashboard, /\.filter\(contact => contact\.favorite\)/)
  assert.match(dashboard, /暂无收藏联系人/)
  assert.doesNotMatch(dashboard, /const quickContacts/)
  assert.doesNotMatch(dashboard, /localeCompare/)
})
