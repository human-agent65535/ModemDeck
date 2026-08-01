import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

import { gateway } from '../src/api/client.ts'

const fixtureApplicationVersion = 'v9.8.7'

const aboutResponse = {
  name: 'ModemDeck',
  version: fixtureApplicationVersion,
  repository_url: 'https://github.com/human-agent65535/ModemDeck',
  license_name: 'PolyForm Noncommercial 1.0.0',
  license_url: 'https://github.com/human-agent65535/ModemDeck/blob/modemdeck/LICENSE',
  notices_url:
    'https://github.com/human-agent65535/ModemDeck/blob/modemdeck/THIRD_PARTY_NOTICES.md'
}

const updateResponse = {
  status: 'unavailable',
  current_version: fixtureApplicationVersion,
  checked_at: '2026-07-28T08:01:00Z',
  error_code: 'github_no_release'
}

test('about gateway uses read-only metadata and update-check endpoints', async () => {
  const originalFetch = globalThis.fetch
  const requests = []
  globalThis.fetch = async input => {
    requests.push(String(input))
    const body = String(input).endsWith('/updates/check') ? updateResponse : aboutResponse
    return new Response(JSON.stringify(body), {
      status: 200,
      headers: { 'Content-Type': 'application/json' }
    })
  }

  try {
    assert.deepEqual(await gateway.getAbout(), aboutResponse)
    const update = await gateway.checkForUpdates()
    assert.equal(update.status, updateResponse.status)
    assert.equal(update.current_version, updateResponse.current_version)
    assert.equal(update.checked_at, updateResponse.checked_at)
    assert.equal(update.release_url, undefined)
  } finally {
    globalThis.fetch = originalFetch
  }

  assert.deepEqual(requests, ['/api/v1/about', '/api/v1/updates/check'])
})

test('update response rejects an unknown status', async () => {
  const originalFetch = globalThis.fetch
  globalThis.fetch = async () =>
    new Response(
      JSON.stringify({
        ...updateResponse,
        status: 'installing'
      }),
      {
        status: 200,
        headers: { 'Content-Type': 'application/json' }
      }
    )
  try {
    await assert.rejects(() => gateway.checkForUpdates(), /status/)
  } finally {
    globalThis.fetch = originalFetch
  }
})

test('about panel checks automatically without an update action and keeps legal links', async () => {
  const [panel, settingsView, notices] = await Promise.all([
    readFile(new URL('../src/components/AboutSettingsPanel.vue', import.meta.url), 'utf8'),
    readFile(new URL('../src/views/SettingsView.vue', import.meta.url), 'utf8'),
    readFile(new URL('../../THIRD_PARTY_NOTICES.md', import.meta.url), 'utf8')
  ])

  assert.match(settingsView, /id: 'about'/)
  assert.match(panel, /gateway\.checkForUpdates\(\)/)
  assert.match(panel, /useInitialLoadBarrier/)
  assert.match(
    panel,
    /waitForInitialLoad\(\[\(\) => load\(\), \(\) => checkForUpdates\(\)\]\)/
  )
  assert.match(panel, /<SettingsLoadBoundary[\s\S]*:loading="initialLoading"/)
  assert.doesNotMatch(panel, /<LoaderCircle/)
  assert.match(panel, /about-status--checking/)
  assert.match(panel, /about\?\.notices_url \|\| noticesURL/)
  assert.doesNotMatch(panel, /about\.checkAgain|installUpdate|downloadUpdate/)
  assert.doesNotMatch(panel, /compactCommit/)
  assert.doesNotMatch(panel, /about\?\.(?:commit|build_date)/)
  assert.match(panel, /about\.projectLicense/)
  assert.match(panel, /about\.thirdPartyNotices/)
  assert.match(notices, /Vue\.js, Vue Router, and Vue I18n/)
})

test('root VERSION is the only maintained release version', async () => {
  const [
    version,
    packageSource,
    packageLockSource,
    makefile,
    hardwareBuilder,
    viteConfig,
    dockerfile,
    fixture
  ] = await Promise.all([
      readFile(new URL('../../VERSION', import.meta.url), 'utf8'),
      readFile(new URL('../package.json', import.meta.url), 'utf8'),
      readFile(new URL('../package-lock.json', import.meta.url), 'utf8'),
      readFile(new URL('../../Makefile', import.meta.url), 'utf8'),
      readFile(new URL('../../hardware/build-image.sh', import.meta.url), 'utf8'),
      readFile(new URL('../vite.config.ts', import.meta.url), 'utf8'),
      readFile(new URL('../../Dockerfile', import.meta.url), 'utf8'),
      readFile(new URL('../src/api/fixture.ts', import.meta.url), 'utf8')
    ])
  const packageDocument = JSON.parse(packageSource)
  const packageLockDocument = JSON.parse(packageLockSource)

  assert.match(version.trim(), /^\d+\.\d+\.\d+$/)
  assert.equal(packageDocument.version, undefined)
  assert.equal(packageLockDocument.version, undefined)
  assert.equal(packageLockDocument.packages[''].version, undefined)
  assert.match(makefile, /RELEASE_VERSION \?=.*< VERSION/)
  assert.match(makefile, /VERSION \?=.*RELEASE_VERSION/)
  assert.match(
    fixture,
    /import\.meta\.env\.VITE_MODEMDECK_BUILD_ID\.trim\(\)/
  )
  assert.match(hardwareBuilder, /< "\$\{repo_root\}\/VERSION"/)
  assert.match(viteConfig, /readFileSync\([\s\S]*new URL\('\.\.\/VERSION'/)
  assert.match(viteConfig, /cssCodeSplit: true/)
  assert.match(dockerfile, /COPY VERSION \/workspace\/VERSION/)
  assert.doesNotMatch(dockerfile, /VITE_MODEMDECK_BUILD_ID=\$\{VCS_REF\}/)
  assert.doesNotMatch(dockerfile, /main\.(?:commit|buildDate)=/)
  assert.match(dockerfile, /org\.opencontainers\.image\.revision="\$\{VCS_REF\}"/)
})
