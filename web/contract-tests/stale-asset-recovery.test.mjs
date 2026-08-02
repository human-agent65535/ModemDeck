import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

import {
  announceApplicationUpdate,
  applicationUpdateState,
  checkForApplicationUpdate,
  clearApplicationUpdateNotice,
  initializeApplicationVersionChecks,
  installStaleAssetRecovery,
  readServerVersion,
  recoverAfterStaleAsset,
  refreshApplication,
  setApplicationUpdateNoticeSuppressed
} from '../src/state/staleAssetRecovery.ts'

function recoveryFixture() {
  const windowListeners = new Map()
  const documentListeners = new Map()
  let reloads = 0

  return {
    document: {
      visibilityState: 'visible',
      addEventListener(name, listener) {
        documentListeners.set(name, listener)
      },
      removeEventListener(name, listener) {
        if (documentListeners.get(name) === listener) documentListeners.delete(name)
      }
    },
    location: {
      reload() {
        reloads += 1
      }
    },
    target: {
      addEventListener(name, listener) {
        windowListeners.set(name, listener)
      },
      removeEventListener(name, listener) {
        if (windowListeners.get(name) === listener) windowListeners.delete(name)
      }
    },
    documentListener(name) {
      return documentListeners.get(name)
    },
    listener(name) {
      return windowListeners.get(name)
    },
    reloads() {
      return reloads
    }
  }
}

async function settle() {
  await Promise.resolve()
  await Promise.resolve()
}

test('a version mismatch publishes a persistent update notice without navigating', async () => {
  const fixture = recoveryFixture()

  assert.equal(
    await checkForApplicationUpdate('v1.8.5', async () => 'v1.8.6'),
    true
  )
  assert.equal(applicationUpdateState.available, true)
  assert.equal(applicationUpdateState.currentVersion, 'v1.8.5')
  assert.equal(applicationUpdateState.serverVersion, 'v1.8.6')
  assert.equal(fixture.reloads(), 0)
})

test('a matching client and server do not announce an update', () => {
  assert.equal(announceApplicationUpdate('v1.8.6', 'v1.8.6'), false)
})

test('the application reloads only after an explicit refresh action', () => {
  const fixture = recoveryFixture()
  refreshApplication(fixture.location)
  assert.equal(fixture.reloads(), 1)
})

test('an administrator-initiated update defers the global refresh notice', () => {
  setApplicationUpdateNoticeSuppressed(true)
  assert.equal(announceApplicationUpdate('v1.8.8', 'v1.8.9'), true)
  assert.equal(applicationUpdateState.available, false)

  setApplicationUpdateNoticeSuppressed(false)
  assert.equal(applicationUpdateState.available, true)
  assert.equal(applicationUpdateState.serverVersion, 'v1.8.9')
  clearApplicationUpdateNotice()
})

test('Web build endpoint returns the container version as the build identity', async () => {
  let request
  const version = await readServerVersion(async (url, options) => {
    request = { url, options }
    return {
      ok: true,
      async json() {
        return { version: 'v1.8.6' }
      }
    }
  })

  assert.equal(version, 'v1.8.6')
  assert.equal(request.url, '/modemdeck-build.json')
  assert.equal(request.options.cache, 'no-store')
  assert.equal(request.options.credentials, 'same-origin')
})

test('failed version checks keep the usable page without reloading it', async () => {
  const fixture = recoveryFixture()
  const recovered = await recoverAfterStaleAsset('v1.8.5', async () => {
    throw new Error('offline')
  })

  assert.equal(recovered, false)
  assert.equal(fixture.reloads(), 0)
})

test('Vite preload failures publish a refresh notice instead of navigating', async () => {
  const fixture = recoveryFixture()
  installStaleAssetRecovery(fixture.target, 'v1.8.5', async () => 'v1.8.6')

  const listener = fixture.listener('vite:preloadError')
  assert.equal(typeof listener, 'function')

  let prevented = false
  await listener({
    preventDefault() {
      prevented = true
    }
  })

  assert.equal(prevented, true)
  assert.equal(applicationUpdateState.serverVersion, 'v1.8.6')
  assert.equal(fixture.reloads(), 0)
})

test('the missing-chunk recovery module can publish the current server version', () => {
  const fixture = recoveryFixture()
  installStaleAssetRecovery(fixture.target, 'v1.8.5')

  fixture.listener('modemdeck:update-ready')({
    detail: { version: 'v1.8.7' }
  })

  assert.equal(applicationUpdateState.currentVersion, 'v1.8.5')
  assert.equal(applicationUpdateState.serverVersion, 'v1.8.7')
  assert.equal(fixture.reloads(), 0)
})

test('lifecycle checks announce an update without waiting for call state', async () => {
  const fixture = recoveryFixture()
  const stop = initializeApplicationVersionChecks(
    fixture.target,
    fixture.document,
    'v1.8.5',
    async () => 'v1.8.8'
  )

  await settle()
  assert.equal(applicationUpdateState.serverVersion, 'v1.8.8')
  assert.equal(fixture.reloads(), 0)

  fixture.listener('pageshow')()
  await settle()
  assert.equal(fixture.reloads(), 0)

  stop()
  assert.equal(fixture.listener('pageshow'), undefined)
  assert.equal(fixture.documentListener('visibilitychange'), undefined)
})

test('the shell renders one global call-aware refresh bar', async () => {
  const [app, bar, main, recovery] = await Promise.all([
    readFile(new URL('../src/App.vue', import.meta.url), 'utf8'),
    readFile(
      new URL('../src/components/ApplicationUpdateBar.vue', import.meta.url),
      'utf8'
    ),
    readFile(new URL('../src/main.ts', import.meta.url), 'utf8'),
    readFile(new URL('../src/state/staleAssetRecovery.ts', import.meta.url), 'utf8')
  ])

  assert.match(app, /<ApplicationUpdateBar \/>/)
  assert.match(bar, /applicationUpdateState\.available/)
  assert.match(
    bar,
    /isLiveCallSession\(session\) && session\.control_state === 'owned'/
  )
  assert.match(bar, /applicationUpdateReadyDuringCall/)
  assert.match(bar, /@click="refreshApplication\(\)"/)
  assert.match(bar, /white-space: nowrap/)
  assert.match(bar, /prefers-reduced-motion: reduce/)
  assert.doesNotMatch(main, /if \(!import\.meta\.env\.DEV && \(await checkForApplicationUpdate\(\)\)\) return/)
  assert.doesNotMatch(recovery, /searchParams|location\.replace/)
})
