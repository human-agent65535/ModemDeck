import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

import {
  communicationAttentionChanges,
  communicationAttentionMessage,
  initializeForegroundCommunicationRefresh
} from '../src/state/foregroundCommunication.ts'

function visibilityFixture(initial = 'visible') {
  let listener
  return {
    target: {
      visibilityState: initial,
      addEventListener(type, current) {
        if (type === 'visibilitychange') listener = current
      },
      removeEventListener(type, current) {
        if (type === 'visibilitychange' && listener === current) listener = undefined
      }
    },
    change(value) {
      this.target.visibilityState = value
      listener?.()
    },
    listener: () => listener
  }
}

test('foreground resume refreshes core communication data and emits one summary', async () => {
  const visibility = visibilityFixture()
  let current = { unreadMessages: 2, missedCalls: 1 }
  let refreshed = { unreadMessages: 5, missedCalls: 3 }
  let refreshes = 0
  const notifications = []
  const stop = initializeForegroundCommunicationRefresh({
    documentTarget: visibility.target,
    snapshot: () => current,
    refresh: async () => {
      refreshes += 1
      current = refreshed
    },
    notify: message => notifications.push(message)
  })

  visibility.change('hidden')
  current = { unreadMessages: 5, missedCalls: 3 }
  refreshed = { unreadMessages: 0, missedCalls: 0 }
  visibility.change('visible')
  await Promise.resolve()
  await Promise.resolve()

  assert.equal(refreshes, 1)
  assert.deepEqual(notifications, ['Unread messages: 3 · New missed calls: 2'])

  visibility.change('visible')
  await Promise.resolve()
  assert.equal(refreshes, 1)

  refreshed = { unreadMessages: 0, missedCalls: 0 }
  visibility.change('hidden')
  visibility.change('visible')
  await Promise.resolve()
  await Promise.resolve()
  assert.equal(refreshes, 2)
  assert.equal(notifications.length, 1)

  stop()
  assert.equal(visibility.listener(), undefined)
})

test('unknown baseline data never turns historical items into resume alerts', () => {
  const changes = communicationAttentionChanges(
    {},
    { unreadMessages: 12, missedCalls: 4 }
  )
  assert.deepEqual(changes, { unreadMessages: 0, missedCalls: 0 })
  assert.equal(communicationAttentionMessage(changes), '')
})

test('the authenticated shell owns the foreground communication lifecycle', async () => {
  const shell = await readFile(
    new URL('../src/components/AppShell.vue', import.meta.url),
    'utf8'
  )
  assert.match(shell, /initializeForegroundCommunicationRefresh\(\)/)
  assert.match(shell, /stopForegroundCommunicationRefresh\?\.\(\)/)
})
