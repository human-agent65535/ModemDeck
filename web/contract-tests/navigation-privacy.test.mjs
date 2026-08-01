import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

import {
  messageComposeContextFromReference,
  messageComposeRoute,
  messageThreadKeyFromReference,
  messageThreadRoute
} from '../src/router/messageRoute.ts'

async function source(path) {
  return readFile(new URL(path, import.meta.url), 'utf8')
}

test('message navigation keeps line identities and phone numbers out of URLs', () => {
  const threadKey = 'line-private|+1 202 555 0199'
  const threadRoute = messageThreadRoute(threadKey)
  assert.equal(threadRoute.name, 'messages')
  assert.match(threadRoute.params.threadRef, /^m_[a-f0-9]{16}$/)
  assert.doesNotMatch(JSON.stringify(threadRoute), /line-private|202 555 0199/)
  assert.equal(
    messageThreadKeyFromReference(threadRoute.params.threadRef),
    threadKey
  )

  const composeRoute = messageComposeRoute({
    recipient: '+1 202 555 0188',
    name: 'Private contact',
    lineKey: 'line-private'
  })
  assert.match(composeRoute.query.compose, /^m_[a-f0-9]{16}$/)
  assert.doesNotMatch(
    JSON.stringify(composeRoute),
    /line-private|202 555 0188|Private contact/
  )
  assert.deepEqual(
    messageComposeContextFromReference(composeRoute.query.compose),
    {
      recipient: '+1 202 555 0188',
      name: 'Private contact',
      lineKey: 'line-private'
    }
  )
})

test('the app uses clean history routes and a geometry-stable auth transition', async () => {
  const [router, app, main, styles] = await Promise.all([
    source('../src/router/index.ts'),
    source('../src/App.vue'),
    source('../src/main.ts'),
    source('../src/style.css')
  ])

  assert.match(router, /createWebHistory\(\)/)
  assert.doesNotMatch(router, /createWebHashHistory/)
  assert.match(router, /path: 'messages\/:threadRef\?'/)
  assert.match(main, /hideApplicationVersion\(\)/)
  assert.match(app, /<Transition name="application-surface">/)
  assert.match(
    app,
    /:key="route\.name === 'login' \? 'login' : 'application'"/
  )
  assert.match(
    styles,
    /\.application-surface-enter-active,[\s\S]*transition: opacity var\(--motion-base\) var\(--ease-standard\)/
  )
})
