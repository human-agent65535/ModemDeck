import assert from 'node:assert/strict'
import test from 'node:test'
import { readFile } from 'node:fs/promises'
import { effectScope, ref } from 'vue'
import { useListArrivals } from '../src/composables/useListArrivals.ts'

test('list arrivals ignore initial data and pagination but mark runtime additions', async () => {
  const items = ref([])
  const ready = ref(false)
  const animate = ref(true)
  const scope = effectScope()
  const arrivals = scope.run(() =>
    useListArrivals(
      () => ({ items: items.value, ready: ready.value, animate: animate.value }),
      item => item.id,
      { holdMilliseconds: 10 }
    )
  )

  items.value = [{ id: 'initial' }]
  ready.value = true
  assert.equal(arrivals.isArriving('initial'), false)

  items.value = [{ id: 'new' }, ...items.value]
  assert.equal(arrivals.isArriving('new'), true)

  animate.value = false
  items.value = [...items.value, { id: 'older-page' }]
  assert.equal(arrivals.isArriving('older-page'), false)

  await new Promise(resolve => setTimeout(resolve, 15))
  assert.equal(arrivals.isArriving('new'), false)
  scope.stop()
})

test('all communication collections use the shared arrival primitive', async () => {
  const row = await readFile(
    new URL('../src/components/SelectableListRow.vue', import.meta.url),
    'utf8'
  )
  assert.match(row, /'is-arriving': arriving/)
  assert.match(row, /prefers-reduced-motion: reduce/)

  for (const view of [
    'ContactsView.vue',
    'MessagesView.vue',
    'CallsView.vue',
    'RecordingsView.vue'
  ]) {
    const source = await readFile(new URL(`../src/views/${view}`, import.meta.url), 'utf8')
    assert.match(source, /useListArrivals/)
    assert.match(source, /:arriving=/)
  }

  const dashboard = await readFile(
    new URL('../src/views/DashboardView.vue', import.meta.url),
    'utf8'
  )
  assert.match(dashboard, /useCommunicationActivity/)
  assert.match(dashboard, /:arriving="activityIsArriving\(activity\)"/)
})
