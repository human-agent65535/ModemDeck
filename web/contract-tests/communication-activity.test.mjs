import assert from 'node:assert/strict'
import test from 'node:test'
import { readFile } from 'node:fs/promises'
import { effectScope, reactive } from 'vue'
import {
  communicationActivities,
  useCommunicationActivity
} from '../src/composables/useCommunicationActivity.ts'
import { messageThreadReference } from '../src/router/messageRoute.ts'

function call(id, startedAt) {
  return {
    id,
    line_id: 'line-main',
    direction: 'incoming',
    remote_number: '+818000000000',
    started_at: startedAt,
    duration_seconds: 0,
    missed: false,
    read: true,
    favorite: false
  }
}

function thread(key, timestamp) {
  return {
    key,
    line_id: 'line-main',
    peer: '+818000000001',
    last_timestamp: timestamp,
    unread_count: 0,
    marked_unread: false,
    favorite: false
  }
}

test('communication activity is one ordered projection of calls and message threads', () => {
  const message = thread('line-main|+818000000001', '2026-08-02T08:02:00Z')
  const activities = communicationActivities(
    [
      call('call-new', '2026-08-02T08:03:00Z'),
      call('call-old', '2026-08-02T08:01:00Z')
    ],
    [message],
    2
  )

  assert.deepEqual(
    activities.map(activity => activity.kind),
    ['call', 'message']
  )
  assert.deepEqual(
    activities.map(activity => activity.key),
    [
      'call:call-new',
      `message:${messageThreadReference(message.key)}`
    ]
  )
})

test('communication activity shares initial-load and live-arrival semantics', () => {
  const calls = reactive({ status: 'loading', data: [], error: '' })
  const threads = reactive({ status: 'loading', data: [], error: '' })
  const recentIncomingThreadKeys = reactive({})
  const scope = effectScope()
  const activity = scope.run(() =>
    useCommunicationActivity({ calls, threads, recentIncomingThreadKeys })
  )

  const initialThread = thread('line-main|+818000000001', '2026-08-02T08:00:00Z')
  threads.data = [initialThread]
  calls.status = 'ready'
  threads.status = 'ready'
  assert.equal(activity.isArriving(activity.activities.value[0]), false)

  const arrivingCall = call('call-new', '2026-08-02T08:01:00Z')
  calls.data = [arrivingCall]
  assert.equal(activity.isArriving(activity.activities.value[0]), true)

  recentIncomingThreadKeys[initialThread.key] = true
  const messageActivity = activity.activities.value.find(item => item.kind === 'message')
  assert.equal(activity.isArriving(messageActivity), true)
  scope.stop()
})

test('dashboard consumes the shared communication activity projection', async () => {
  const dashboard = await readFile(
    new URL('../src/views/DashboardView.vue', import.meta.url),
    'utf8'
  )

  assert.match(dashboard, /useCommunicationActivity/)
  assert.match(dashboard, /activityIsArriving\(activity\)/)
  assert.doesNotMatch(dashboard, /const activities = computed<DashboardActivity\[\]>/)
})
