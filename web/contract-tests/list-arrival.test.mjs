import assert from 'node:assert/strict'
import test from 'node:test'
import { readFile } from 'node:fs/promises'
import { effectScope, reactive, ref, watch } from 'vue'
import {
  gateway,
  rotateAuthenticationRequestScope
} from '../src/api/client.ts'
import {
  initializeMessageRuntime,
  shutdownMessageRuntime
} from '../src/state/messageRuntime.ts'
import {
  loadThreads,
  recentIncomingThreadKeys,
  refreshMessageWorkspace,
  resetWorkspaceState,
  threadsResource
} from '../src/state/workspace.ts'
import { useCommunicationActivity } from '../src/composables/useCommunicationActivity.ts'
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

test('incoming SMS ignores a stale thread response before its arrival animation begins', async () => {
  const originalFetch = globalThis.fetch
  const originalSubscribe = gateway.subscribeMessageEvents
  const threadKey = 'line-main|+818012345678'
  const otherThreadKey = 'line-main|+818000000000'
  const now = Date.now()
  const initialThreads = [
    {
      key: otherThreadKey,
      line_id: 'line-main',
      peer: '+818000000000',
      last_message_id: '20',
      last_timestamp: new Date(now - 60_000).toISOString(),
      last_content: 'newer before SSE',
      unread_count: 0,
      marked_unread: false,
      favorite: false
    },
    {
      key: threadKey,
      line_id: 'line-main',
      peer: '+818012345678',
      last_message_id: '10',
      last_timestamp: new Date(now - 120_000).toISOString(),
      last_content: 'older before SSE',
      unread_count: 0,
      marked_unread: false,
      favorite: false
    }
  ]
  const liveTimestamp = new Date(now).toISOString()
  const refreshedThreads = [
    {
      ...initialThreads[1],
      last_message_id: '30',
      last_timestamp: liveTimestamp,
      last_content: 'live SMS',
      unread_count: 1
    },
    initialThreads[0]
  ]
  const response = (threads, etag) => new Response(JSON.stringify({
    threads,
    meta: { limit: 50, next_cursor: '', has_more: false }
  }), {
    status: 200,
    headers: {
      'Content-Type': 'application/json',
      ETag: etag
    }
  })
  const requests = []
  let handlers
  let releaseStaleRefresh
  let notifyStaleRefreshRequested
  const staleRefreshRequested = new Promise(resolve => {
    notifyStaleRefreshRequested = resolve
  })
  let releaseFreshRefresh
  let notifyFreshRefreshRequested
  const freshRefreshRequested = new Promise(resolve => {
    notifyFreshRefreshRequested = resolve
  })
  let stopSnapshots = () => undefined
  let activityScope

  try {
    shutdownMessageRuntime()
    resetWorkspaceState()
    rotateAuthenticationRequestScope()
    globalThis.fetch = async (input, init) => {
      const headers = new Headers(init?.headers)
      requests.push({ input: String(input), headers })
      if (requests.length === 1) {
        assert.equal(headers.get('If-None-Match'), null)
        return response(initialThreads, 'W/"threads-1"')
      }
      if (requests.length === 2) {
        assert.equal(headers.get('If-None-Match'), 'W/"threads-1"')
        notifyStaleRefreshRequested()
        return new Promise(resolve => {
          releaseStaleRefresh = () => resolve(new Response(null, {
            status: 304,
            headers: { ETag: 'W/"threads-1"' }
          }))
        })
      }
      if (requests.length === 3) {
        assert.equal(headers.get('If-None-Match'), 'W/"threads-1"')
        notifyFreshRefreshRequested()
        return new Promise(resolve => {
          releaseFreshRefresh = () => resolve(response(refreshedThreads, 'W/"threads-2"'))
        })
      }
      assert.equal(headers.get('If-None-Match'), 'W/"threads-2"')
      return new Response(null, {
        status: 304,
        headers: { ETag: 'W/"threads-2"' }
      })
    }
    gateway.subscribeMessageEvents = currentHandlers => {
      handlers = currentHandlers
      currentHandlers.onOpen?.()
      return () => undefined
    }

    await loadThreads(true)
    assert.equal(threadsResource.data[0]?.key, otherThreadKey)

    const calls = reactive({ status: 'ready', data: [], error: '' })
    activityScope = effectScope()
    const activity = activityScope.run(() =>
      useCommunicationActivity({
        calls,
        threads: threadsResource,
        recentIncomingThreadKeys
      })
    )
    const snapshots = []
    let notifyArrivalObserved
    const arrivalObserved = new Promise(resolve => {
      notifyArrivalObserved = resolve
    })
    stopSnapshots = watch(
      () => {
        const first = activity.activities.value[0]
        return {
          key: first?.kind === 'message' ? first.thread.key : '',
          arriving: first ? activity.isArriving(first) : false
        }
      },
      snapshot => {
        snapshots.push(snapshot)
        if (snapshot.key === threadKey && snapshot.arriving) {
          notifyArrivalObserved()
        }
      },
      { immediate: true, flush: 'sync' }
    )

    const staleRefresh = refreshMessageWorkspace('')
    await staleRefreshRequested

    initializeMessageRuntime({
      currentRoute: { value: { name: 'dashboard', params: {}, query: {} } }
    })
    handlers.onMessage({
      message_id: '30',
      thread_key: threadKey,
      line_id: 'line-main',
      peer: '+818012345678',
      content: 'live SMS',
      timestamp: liveTimestamp,
      observed_at: new Date().toISOString()
    })

    assert.equal(threadsResource.data[0]?.key, otherThreadKey)
    assert.equal(recentIncomingThreadKeys[threadKey], undefined)

    releaseStaleRefresh()
    await freshRefreshRequested
    assert.equal(threadsResource.data[0]?.key, otherThreadKey)
    assert.equal(recentIncomingThreadKeys[threadKey], undefined)

    releaseFreshRefresh()
    await arrivalObserved
    await staleRefresh
    assert.equal(threadsResource.data[0]?.key, threadKey)
    assert.equal(activity.activities.value[0].thread.key, threadKey)
    assert.equal(activity.isArriving(activity.activities.value[0]), true)
    assert.equal(
      snapshots.some(snapshot => snapshot.key === otherThreadKey && snapshot.arriving),
      false
    )
    const reordered = snapshots.findIndex(
      snapshot => snapshot.key === threadKey && !snapshot.arriving
    )
    const animated = snapshots.findIndex(
      snapshot => snapshot.key === threadKey && snapshot.arriving
    )
    assert.ok(reordered >= 0)
    assert.ok(animated > reordered)

    const cached = await gateway.listThreads()
    assert.equal(cached.items[0]?.key, threadKey)
    assert.equal(requests.length, 4)
  } finally {
    stopSnapshots()
    activityScope?.stop()
    shutdownMessageRuntime()
    gateway.subscribeMessageEvents = originalSubscribe
    globalThis.fetch = originalFetch
    resetWorkspaceState()
    rotateAuthenticationRequestScope()
  }
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
