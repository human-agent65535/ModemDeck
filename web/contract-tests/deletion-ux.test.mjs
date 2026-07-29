import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

async function source(path) {
  return readFile(new URL(path, import.meta.url), 'utf8')
}

test('phone and tablet list rows expose native-style swipe actions', async () => {
  const swipeRow = await source('../src/components/SwipeActionRow.vue')

  assert.match(swipeRow, /window\.matchMedia\('\(max-width: 1100px\)'\)\.matches/)
  assert.match(swipeRow, /event\.pointerType !== 'touch' && event\.pointerType !== 'pen'/)
  assert.match(swipeRow, /side === 'read' \? ACTION_WIDTH : -ACTION_WIDTH/)
  assert.match(swipeRow, /@media \(max-width: 1100px\)/)
  assert.match(
    swipeRow,
    /\.swipe-action-row__surface \{\s*touch-action: pan-y;/
  )
  assert.match(
    swipeRow,
    /\.swipe-action-row__action \{\s*display: flex;/
  )
})

test('phone and tablet details defer deletion to the swipe action', async () => {
  const views = await Promise.all([
    source('../src/views/MessagesView.vue'),
    source('../src/views/CallsView.vue'),
    source('../src/views/ContactsView.vue'),
    source('../src/views/RecordingsView.vue')
  ])

  for (const view of views) {
    assert.match(view, /class="icon-button icon-button--danger desktop-delete-action"/)
    assert.match(
      view,
      /@media \(max-width: 1100px\)[\s\S]*?\.desktop-delete-action \{\s*display: none;/
    )
  }
})

test('dashboard activity exposes the same read and delete actions as communication lists', async () => {
  const [dashboard, messages, calls] = await Promise.all([
    source('../src/views/DashboardView.vue'),
    source('../src/views/MessagesView.vue'),
    source('../src/views/CallsView.vue')
  ])

  assert.match(dashboard, /import SwipeActionRow from/)
  assert.match(
    dashboard,
    /<SwipeActionRow[\s\S]*v-for="activity in activities"[\s\S]*:can-read="activityCanRead\(activity\)"[\s\S]*@read="markActivityRead\(activity\)"[\s\S]*@delete="removeActivity\(activity\)"/
  )
  assert.match(dashboard, /await deleteMessageThread\(activity\.thread\)/)
  assert.match(dashboard, /await deleteCall\(activity\.call\)/)
  assert.match(dashboard, /forgetCallRecordings\(activity\.call\.id\)/)
  assert.match(
    dashboard,
    /selectionKey\.value === activity\.key[\s\S]*router\.replace\(\{ name: 'dashboard' \}\)/
  )
  assert.doesNotMatch(messages, /selectedThread && !composingNew && !embedded/)
  assert.doesNotMatch(
    calls,
    /<button\s+v-if="!embedded"\s+class="icon-button icon-button--danger desktop-delete-action"/
  )
})
