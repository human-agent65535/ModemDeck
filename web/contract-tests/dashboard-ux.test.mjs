import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const dashboard = await readFile(
  new URL('../src/views/DashboardView.vue', import.meta.url),
  'utf8'
)
const contacts = await readFile(
  new URL('../src/views/ContactsView.vue', import.meta.url),
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

test('dashboard keeps creation actions with activity and omits the duplicate overview header', () => {
  const activityHeaderStart = dashboard.indexOf('<header class="pane-header">')
  const activityHeaderEnd = dashboard.indexOf('</header>', activityHeaderStart)
  const activityHeader = dashboard.slice(activityHeaderStart, activityHeaderEnd)

  assert.equal((dashboard.match(/@click="composeMessage"/g) || []).length, 1)
  assert.match(activityHeader, /@click="composeMessage"/)
  assert.match(activityHeader, /@click="createContact"/)
  assert.match(activityHeader, /<MessageSquareText/)
  assert.match(activityHeader, /<UserPlus/)
  assert.doesNotMatch(dashboard, /dashboard-detail-header/)
  assert.doesNotMatch(dashboard, /dashboard-header-actions/)
  assert.doesNotMatch(dashboard, /dashboard-command-button/)
  assert.match(dashboard, /mobile-back dashboard-overview-back/)
  assert.doesNotMatch(dashboard, /\bSettings\b/)
  assert.match(
    dashboard,
    /id="dashboard-lines-title"[\s\S]*t\('dashboard\.manageDevices'\)/
  )
})

test('dashboard new-contact action opens the shared contacts editor flow', () => {
  assert.match(
    dashboard,
    /router\.push\(\{ name: 'contacts', query: \{ create: '1' \} \}\)/
  )
  assert.doesNotMatch(dashboard, /import ContactEditor/)
  assert.match(contacts, /\(\) => route\.query\.create/)
  assert.match(contacts, /requested !== '1'[\s\S]*openNew\(\)/)
  assert.equal((contacts.match(/<ContactEditor/g) || []).length, 1)
})

test('dashboard uses existing network data for a traffic summary and entry point', () => {
  assert.match(dashboard, /import \{ loadNetwork, networkState \} from '\.\.\/state\/network'/)
  assert.match(dashboard, /loadNetwork\(\)/)
  assert.match(dashboard, /<TrafficSummary/)
  assert.match(dashboard, /:today-bytes="todayTraffic"/)
  assert.match(dashboard, /:month-bytes="monthTraffic"/)
  assert.match(dashboard, /:to="\{ name: 'traffic' \}"/)
})

test('dashboard readiness counts require both service and backend capability', () => {
  assert.match(dashboard, /isVoiceServiceReady\(line\)/)
  assert.match(dashboard, /isMessagingServiceReady\(line\)/)
  assert.match(
    dashboard,
    /isVoiceServiceReady\(line\)\s*&&\s*\(line\.capabilities\?\.dial === true \|\| line\.capabilities\?\.voice === true\)/
  )
  assert.match(
    dashboard,
    /isMessagingServiceReady\(line\)\s*&&\s*\(line\.capabilities\?\.message === true \|\|\s*line\.capabilities\?\.messaging === true\)/
  )
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
  assert.match(dashboard, /t\('dashboard\.noFavoriteContacts'\)/)
  assert.doesNotMatch(dashboard, /const quickContacts/)
  assert.doesNotMatch(dashboard, /localeCompare/)
})

test('dashboard keeps favorite initials centered and reuses contact actions for activities', () => {
  assert.match(dashboard, /class="dashboard-favorite-avatar"/)
  assert.match(
    dashboard,
    /\.dashboard-contact-row > a > :deep\(\.dashboard-favorite-avatar\)[\s\S]*?place-items: center/
  )
  assert.match(dashboard, /import ContactHeaderIdentity/)
  assert.match(dashboard, /import ContactNumberActions/)
  assert.match(
    dashboard,
    /<ContactNumberActions\s+:number="selectedCall\.remote_number"\s+:contact="contactForNumber\(selectedCall\.remote_number\)"/
  )
  assert.match(
    dashboard,
    /<ContactNumberActions\s+:number="selectedThread\.peer"\s+:contact="contactForNumber\(selectedThread\.peer\)"/
  )
})
