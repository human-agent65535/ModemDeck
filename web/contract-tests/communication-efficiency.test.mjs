import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { smsLength } from '../src/utils/smsSegments.ts'
import { formatDialInput, stripDialFormatting } from '../src/utils/format.ts'

async function source(path) {
  return readFile(new URL(path, import.meta.url), 'utf8')
}

test('SMS length follows GSM-7 and UCS-2 part sizes', () => {
  assert.deepEqual(smsLength(''), { encoding: 'gsm7', units: 0, segments: 0, limit: 160 })
  assert.equal(smsLength('a'.repeat(160)).segments, 1)
  assert.deepEqual(smsLength('a'.repeat(161)), { encoding: 'gsm7', units: 161, segments: 2, limit: 306 })
  // Escape-table characters cost two septets.
  assert.equal(smsLength('€'.repeat(80)).units, 160)
  assert.equal(smsLength('€'.repeat(81)).segments, 2)
  // Any character outside GSM-7 switches the whole message to UCS-2.
  assert.deepEqual(smsLength('你好'.repeat(35)), { encoding: 'ucs2', units: 70, segments: 1, limit: 70 })
  assert.equal(smsLength('你好'.repeat(36)).segments, 2)
})

test('the dialer groups plain numbers as typed and leaves service codes alone', () => {
  assert.equal(formatDialInput('2025550107', 'US'), '(202) 555-0107')
  assert.equal(formatDialInput('+12025550107'), '+1 202 555 0107')
  assert.equal(formatDialInput('*#06#', 'US'), '*#06#')
  assert.equal(formatDialInput('123', 'US'), '123')
  assert.equal(stripDialFormatting('(202) 555-0107'), '2025550107')
  assert.equal(stripDialFormatting('+1 202 555 0107'), '+12025550107')
})

test('lists move with the arrow keys and the shell offers single-key shortcuts', async () => {
  const [shell, dialog] = await Promise.all([
    source('../src/components/AppShell.vue'),
    source('../src/components/KeyboardShortcutsDialog.vue')
  ])

  assert.match(shell, /const row = \(event\.target as HTMLElement \| null\)\?\.closest\?\.<HTMLElement>\('\.list-item'\)/)
  assert.match(shell, /if \(event\.key === 'ArrowDown'\) next = Math\.min\(rows\.length - 1, index \+ 1\)/)
  // Single-key shortcuts never fire while typing or behind a modal.
  assert.match(shell, /if \(editableTarget\(event\.target\) \|\| document\.querySelector\('\[aria-modal="true"\]'\)\) return/)
  assert.match(shell, /event\.key\.toLowerCase\(\) === 'c'[\s\S]*messageComposeRoute/)
  assert.match(shell, /event\.key\.toLowerCase\(\) === 'd'[\s\S]*openDialer\(\)/)
  assert.match(shell, /<KeyboardShortcutsDialog :open="shortcutsOpen" @close="shortcutsOpen = false" \/>/)
  assert.match(dialog, /t\('shortcuts\.moveInList'\)/)
})

test('detail headers keep one primary action, a visible favorite and a More menu', async () => {
  const [actions, contacts, calls] = await Promise.all([
    source('../src/components/workspace/WorkspaceDetailActions.vue'),
    source('../src/views/ContactsView.vue'),
    source('../src/views/CallsView.vue')
  ])

  assert.match(actions, /\.workspace-detail-actions__more \{\s*display: inline-grid;\s*\}/)
  assert.doesNotMatch(actions, /@container \(max-width: 560px\)/)
  assert.match(actions, /<slot name="trailing" \/>[\s\S]*workspace-detail-actions__more[\s\S]*<slot name="secondary" \/>/)
  assert.match(contacts, /class="workspace-detail-command workspace-detail-command--primary"[\s\S]*@click="call\(selected, selectedPrimaryNumber\)"/)
  assert.match(contacts, /<template v-if="contactEditingAvailable" #secondary>[\s\S]*openEdit\(selected\)[\s\S]*remove\(selected\)/)
  assert.match(contacts, /<RecentActivitySection/)
  assert.match(calls, /<RecentActivitySection\s*:numbers="\[selected\.remote_number\]"\s*:exclude-call-id="selected\.id"/)
})

test('unread rows carry weight, counts follow filters and empty results can be cleared', async () => {
  const [styles, thread, callRow, calls, messages, recordings] = await Promise.all([
    source('../src/style.css'),
    source('../src/components/MessageThreadListItem.vue'),
    source('../src/components/CallHistoryListItem.vue'),
    source('../src/views/CallsView.vue'),
    source('../src/views/MessagesView.vue'),
    source('../src/views/RecordingsView.vue')
  ])

  assert.match(thread, /'is-unread': thread\.unread_count > 0 \|\| thread\.marked_unread/)
  assert.match(styles, /:is\(\.list-item--thread, \.call-list-item\)\.is-unread \.list-item__content strong \{\s*font-weight: var\(--weight-strong\);/)
  assert.match(callRow, /<PhoneMissed v-if="call\.missed" class="call-list-item__direction"/)
  assert.match(calls, /\? filteredCalls\.length/)
  assert.match(messages, /\? filteredThreads\.length/)
  assert.match(recordings, /\? filteredRecordings\.length/)
  for (const view of [calls, messages, recordings]) {
    assert.match(view, /function clearFilters\(\): void \{/)
    assert.match(view, /@click="clearFilters"/)
  }
  assert.match(messages, /:aria-label="t\('messages\.enterMessage'\)"/)
  assert.match(messages, /t\('messages\.smsLength', \{/)
})
