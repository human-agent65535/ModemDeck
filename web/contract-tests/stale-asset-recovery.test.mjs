import assert from 'node:assert/strict'
import test from 'node:test'

import {
  checkForApplicationUpdate,
  initializeApplicationVersionChecks,
  installStaleAssetRecovery,
  readServerVersion,
  recoverAfterStaleAsset,
  switchApplicationVersion,
  versionedEntryURL
} from '../src/state/staleAssetRecovery.ts'

function recoveryFixture(href = 'https://modemdeck.test/#/calls') {
  const values = new Map()
  const windowListeners = new Map()
  const documentListeners = new Map()
  const replacements = []
  const location = {
    href,
    replace(value) {
      replacements.push(value)
      location.href = value
    }
  }

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
    target: {
      addEventListener(name, listener) {
        windowListeners.set(name, listener)
      },
      removeEventListener(name, listener) {
        if (windowListeners.get(name) === listener) windowListeners.delete(name)
      },
      location,
      sessionStorage: {
        getItem(key) {
          return values.get(key) ?? null
        },
        setItem(key, value) {
          values.set(key, value)
        }
      }
    },
    documentListener(name) {
      return documentListeners.get(name)
    },
    listener(name) {
      return windowListeners.get(name)
    },
    replacements
  }
}

async function settle() {
  await Promise.resolve()
  await Promise.resolve()
}

test('versioned entry URL preserves the active hash route and other query values', () => {
  assert.equal(
    versionedEntryURL(
      'https://modemdeck.test/?source=home#/settings/account?user=1',
      'v1.8.6'
    ),
    'https://modemdeck.test/?source=home&v=v1.8.6#/settings/account?user=1'
  )
})

test('application version switches at most once for each build transition', () => {
  const fixture = recoveryFixture()

  assert.equal(
    switchApplicationVersion(fixture.target, 'v1.8.5', 'v1.8.6'),
    true
  )
  assert.deepEqual(fixture.replacements, [
    'https://modemdeck.test/?v=v1.8.6#/calls'
  ])
  assert.equal(
    switchApplicationVersion(fixture.target, 'v1.8.5', 'v1.8.6'),
    false
  )
  assert.equal(fixture.replacements.length, 1)

  assert.equal(
    switchApplicationVersion(fixture.target, 'v1.8.6', 'v1.8.7'),
    true
  )
  assert.equal(fixture.replacements.length, 2)
})

test('storage failure permits a cache-key change but never reloads the same entry', () => {
  const fixture = recoveryFixture()
  fixture.target.sessionStorage.getItem = () => {
    throw new Error('storage unavailable')
  }

  assert.equal(
    switchApplicationVersion(fixture.target, 'v1.8.5', 'v1.8.6'),
    true
  )
  assert.equal(fixture.replacements.length, 1)
  assert.equal(
    switchApplicationVersion(fixture.target, 'v1.8.5', 'v1.8.6'),
    false
  )
  assert.equal(fixture.replacements.length, 1)
})

test('version endpoint returns the release version as the build identity', async () => {
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
  assert.equal(request.url, '/api/v1/version')
  assert.equal(request.options.cache, 'no-store')
  assert.equal(request.options.credentials, 'same-origin')
})

test('failed version checks keep the usable page instead of navigating offline', async () => {
  const fixture = recoveryFixture()
  const recovered = await recoverAfterStaleAsset(
    fixture.target,
    'v1.8.5',
    async () => {
      throw new Error('offline')
    }
  )

  assert.equal(recovered, false)
  assert.equal(fixture.replacements.length, 0)
})

test('Vite preload failures verify the server version and replace once', async () => {
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
  assert.equal(fixture.replacements.length, 1)

  await listener({ preventDefault() {} })
  assert.equal(fixture.replacements.length, 1)
})

test('lifecycle checks defer during a call and switch when the page is safe', async () => {
  const fixture = recoveryFixture()
  let safe = false
  const stop = initializeApplicationVersionChecks(
    fixture.target,
    fixture.document,
    () => safe,
    'v1.8.5',
    async () => 'v1.8.6'
  )

  await settle()
  assert.equal(fixture.replacements.length, 0)

  safe = true
  fixture.listener('pageshow')()
  await settle()
  assert.equal(fixture.replacements.length, 1)

  stop()
  assert.equal(fixture.listener('pageshow'), undefined)
  assert.equal(fixture.documentListener('visibilitychange'), undefined)
})

test('proactive checks do nothing when the client already matches the server', async () => {
  const fixture = recoveryFixture('https://modemdeck.test/?v=v1.8.6#/')
  assert.equal(
    await checkForApplicationUpdate(
      fixture.target,
      'v1.8.6',
      async () => 'v1.8.6'
    ),
    false
  )
  assert.equal(fixture.replacements.length, 0)
})

test('proactive checks add the version cache key to an unversioned entry', async () => {
  const fixture = recoveryFixture()
  assert.equal(
    await checkForApplicationUpdate(
      fixture.target,
      'v1.8.6',
      async () => 'v1.8.6'
    ),
    true
  )
  assert.deepEqual(fixture.replacements, [
    'https://modemdeck.test/?v=v1.8.6#/calls'
  ])
})
