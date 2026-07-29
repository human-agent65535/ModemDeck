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
