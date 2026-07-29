import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const dashboard = await readFile(
  new URL('../src/views/DashboardView.vue', import.meta.url),
  'utf8'
)
const messages = await readFile(
  new URL('../src/views/MessagesView.vue', import.meta.url),
  'utf8'
)
const calls = await readFile(
  new URL('../src/views/CallsView.vue', import.meta.url),
  'utf8'
)
const messageThreadListItem = await readFile(
  new URL('../src/components/MessageThreadListItem.vue', import.meta.url),
  'utf8'
)
const callHistoryListItem = await readFile(
  new URL('../src/components/CallHistoryListItem.vue', import.meta.url),
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

test('dashboard new-contact action opens the shared editor in place', () => {
  assert.match(dashboard, /import ContactEditor from/)
  assert.match(dashboard, /const contactEditorOpen = ref\(false\)/)
  assert.match(dashboard, /contactEditorOpen\.value = true/)
  assert.match(dashboard, /await saveContact\(input\)/)
  assert.match(dashboard, /<ContactEditor[\s\S]*:open="contactEditorOpen"/)
  assert.match(dashboard, /@save="saveNewContact"/)
  assert.doesNotMatch(dashboard, /query: \{ create: '1' \}/)
})

test('dashboard reuses message and call detail surfaces in the middle pane', () => {
  assert.match(dashboard, /import CallsView from/)
  assert.match(dashboard, /import MessagesView from/)
  assert.match(
    dashboard,
    /<MessagesView[\s\S]*v-if="composingMessage"[\s\S]*embedded-compose/
  )
  assert.match(
    dashboard,
    /<MessagesView[\s\S]*v-else-if="selectedThread"[\s\S]*:embedded-thread-key="selectedThread\.key"/
  )
  assert.match(
    dashboard,
    /<CallsView[\s\S]*v-else-if="selectedCall"[\s\S]*:embedded-call-id="selectedCall\.id"/
  )
  assert.doesNotMatch(dashboard, /<template v-else-if="selectedThread">/)
  assert.doesNotMatch(dashboard, /<template v-else-if="selectedCall">/)
  assert.doesNotMatch(dashboard, /dashboard-message-detail/)

  assert.match(messages, /embeddedThreadKey\?: string/)
  assert.match(
    messages,
    /props\.embeddedThreadKey \|\| String\(route\.params\.threadKey \|\| ''\)/
  )
  assert.match(messages, /<aside v-if="!embedded" class="list-pane">/)
  assert.match(calls, /embeddedCallId\?: string/)
  assert.match(calls, /props\.embeddedCallId \|\|/)
  assert.match(calls, /<aside v-if="!embedded" class="list-pane">/)
})

test('dashboard activity reuses the same message and call list items as their modules', () => {
  assert.match(dashboard, /import MessageThreadListItem from/)
  assert.match(dashboard, /import CallHistoryListItem from/)
  assert.match(messages, /import MessageThreadListItem from/)
  assert.match(calls, /import CallHistoryListItem from/)
  assert.match(
    dashboard,
    /<MessageThreadListItem[\s\S]*?:thread="activity\.thread"[\s\S]*?:selected="selectionKey === activity\.key"/
  )
  assert.match(
    dashboard,
    /<CallHistoryListItem[\s\S]*?:call="activity\.call"[\s\S]*?:selected="selectionKey === activity\.key"/
  )
  assert.match(messages, /<MessageThreadListItem[\s\S]*?@select="chooseThread"/)
  assert.match(calls, /<CallHistoryListItem[\s\S]*?@select="selectCall"/)
  assert.match(messageThreadListItem, /class="list-item list-item--thread"/)
  assert.match(callHistoryListItem, /class="list-item call-list-item"/)
  assert.doesNotMatch(dashboard, /dashboard-activity-row/)
  assert.doesNotMatch(dashboard, /dashboard-activity-meta/)
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
    /isVoiceServiceReady\(line\)\s*&&\s*lineCanPlaceVoiceCall\(line\)/
  )
  assert.match(
    dashboard,
    /isMessagingServiceReady\(line\)\s*&&\s*\(line\.capabilities\?\.message === true \|\|\s*line\.capabilities\?\.messaging === true\)/
  )
})

test('dashboard reports physical modem inventory separately from service readiness', () => {
  assert.match(dashboard, /presentModuleLines\(lines\.value, devicesResource\.data\)/)
  assert.match(
    dashboard,
    /presentModules\.value\.filter\(line => isRegisteredNetwork\(line\)\)/
  )
  assert.match(
    dashboard,
    /t\('dashboard\.modulesOnline', \{[\s\S]*online: onlineModules,[\s\S]*total: presentModules\.length/
  )
  assert.match(
    dashboard,
    /\{\{ onlineModules \}\}<small>\/\{\{ presentModules\.length \}\}<\/small>/
  )
  assert.match(
    dashboard,
    /t\('dashboard\.moduleStatus'\)[\s\S]*\{\{ presentModules\.length \}\}[\s\S]*t\('device\.modules'\)/
  )
  assert.doesNotMatch(dashboard, /attentionCount|dashboard\.attention/)
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

test('dashboard keeps favorite initials centered and delegated details retain contact actions', () => {
  assert.match(dashboard, /class="dashboard-favorite-avatar"/)
  assert.match(
    dashboard,
    /\.dashboard-contact-row > a > :deep\(\.dashboard-favorite-avatar\)[\s\S]*?place-items: center/
  )
  assert.match(messages, /import ContactNumberActions/)
  assert.match(calls, /import ContactNumberActions/)
  assert.match(
    calls,
    /<ContactNumberActions[\s\S]*:number="selected\.remote_number"[\s\S]*:contact="selectedContact"/
  )
  assert.match(
    messages,
    /<ContactNumberActions[\s\S]*:number="selectedThread\.peer"[\s\S]*:contact="activeContact"/
  )
})
