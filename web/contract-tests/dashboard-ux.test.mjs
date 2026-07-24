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

test('dashboard entity grids retain stable desktop columns and card widths', () => {
  const entityGridBlock =
    dashboard.match(
      /\.dashboard-module-grid,\s*\.dashboard-detail-list\s*\{([^}]*)\}/
    )?.[1] || ''
  const entityCardBlock =
    dashboard.match(
      /\.dashboard-module-grid > :deep\(\.module-card\),\s*\.dashboard-detail-list > \.dashboard-contact-row\s*\{([^}]*)\}/
    )?.[1] || ''

  assert.match(
    entityGridBlock,
    /grid-template-columns: repeat\(auto-fill, minmax\(320px, 420px\)\)/
  )
  assert.match(entityGridBlock, /justify-content: start/)
  assert.match(entityCardBlock, /width: 100%/)
  assert.match(entityCardBlock, /max-width: 420px/)
})

test('dashboard summary cards may fill their grid while entity cards fill only narrow screens', () => {
  const summaryGridBlock =
    dashboard.match(/\.dashboard-summary-grid\s*\{([^}]*)\}/)?.[1] || ''
  const narrowMedia =
    dashboard.match(/@media \(max-width: 640px\) \{([\s\S]*?)\n\}/)?.[1] || ''

  assert.match(
    summaryGridBlock,
    /grid-template-columns: repeat\(4, minmax\(0, 1fr\)\)/
  )
  assert.doesNotMatch(summaryGridBlock, /max-width: 420px/)
  assert.match(
    narrowMedia,
    /\.dashboard-module-grid,\s*\.dashboard-detail-list\s*\{[\s\S]*?grid-template-columns: minmax\(0, 1fr\)/
  )
  assert.match(
    narrowMedia,
    /\.dashboard-module-grid > :deep\(\.module-card\),\s*\.dashboard-detail-list > \.dashboard-contact-row\s*\{[\s\S]*?max-width: none/
  )
})

test('dashboard favorites never substitute recent or alphabetic contacts', () => {
  assert.match(dashboard, /const favoriteContacts = computed/)
  assert.match(dashboard, /\.filter\(contact => contact\.favorite\)/)
  assert.match(dashboard, /暂无收藏联系人/)
  assert.doesNotMatch(dashboard, /const quickContacts/)
  assert.doesNotMatch(dashboard, /localeCompare/)
})
