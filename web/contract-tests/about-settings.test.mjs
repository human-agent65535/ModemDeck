import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

import { gateway } from '../src/api/client.ts'
import { createFixtureGateway } from '../src/api/fixture.ts'
import {
  parseReleaseNotes,
  releaseNoteRemainder,
  releaseNoteSummary
} from '../src/utils/releaseNotes.ts'

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
  error_code: 'github_no_release',
  release_notes: '## Highlights\n\n- First outcome',
  apply_available: false,
  hardware_confirmation_required: false
}

test('about gateway uses read-only metadata and update-check endpoints', async () => {
  const originalFetch = globalThis.fetch
  const requests = []
  globalThis.fetch = async input => {
    requests.push(String(input))
    const body = String(input).includes('/updates/check') ? updateResponse : aboutResponse
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
    assert.equal(update.release_notes, updateResponse.release_notes)
    await gateway.checkForUpdates(true)
  } finally {
    globalThis.fetch = originalFetch
  }

  assert.deepEqual(requests, [
    '/api/v1/about',
    '/api/v1/updates/check',
    '/api/v1/updates/check?refresh=1'
  ])
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

test('runtime event stream reports per-container update state', () => {
  const originalEventSource = globalThis.EventSource
  class FakeEventSource {
    static instances = []
    listeners = new Map()
    closed = false

    constructor(url, options) {
      this.url = url
      this.withCredentials = options?.withCredentials
      FakeEventSource.instances.push(this)
    }

    addEventListener(type, listener) {
      this.listeners.set(type, listener)
    }

    emit(type, data) {
      this.listeners.get(type)?.({ data })
    }

    close() {
      this.closed = true
    }
  }
  globalThis.EventSource = FakeEventSource

  try {
    let observed
    const close = gateway.subscribeRuntimeEvents({
      onOpen: () => undefined,
      onHeartbeat: () => undefined,
      onState: () => undefined,
      onUpdateOperation: operation => {
        observed = operation
      },
      onError: () => undefined
    }, 'operation-1')
    assert.equal(
      FakeEventSource.instances[0].url,
      '/api/v1/runtime/events?update_operation=operation-1'
    )
    assert.equal(FakeEventSource.instances[0].withCredentials, true)
    FakeEventSource.instances[0].emit(
      'update',
      JSON.stringify({
        id: 'operation-1',
        state: 'running',
        target_version: 'v1.9.3',
        started_at: '2026-08-02T00:00:00Z',
        components: [
          { name: 'api', state: 'ready' },
          { name: 'web', state: 'restarting' }
        ]
      })
    )
    assert.deepEqual(observed?.components, [
      { name: 'api', state: 'ready' },
      { name: 'web', state: 'restarting' }
    ])
    close()
    assert.equal(FakeEventSource.instances[0].closed, true)
  } finally {
    if (originalEventSource === undefined) delete globalThis.EventSource
    else globalThis.EventSource = originalEventSource
  }
})

test('fixture can preview regular and Hardware update states', async () => {
  const regular = await createFixtureGateway({ initialSoftwareUpdate: true }).checkForUpdates()
  assert.equal(regular.status, 'update_available')
  assert.equal(regular.current_version, 'v1.9.2')
  assert.equal(regular.latest_version, 'v1.9.3')
  assert.equal(regular.apply_available, true)
  assert.equal(regular.hardware_confirmation_required, false)
  assert.deepEqual(
    regular.components?.filter(component => component.changed).map(component => component.name),
    ['api', 'web', 'updater']
  )

  const hardware = await createFixtureGateway({
    initialSoftwareUpdate: 'hardware'
  }).checkForUpdates()
  assert.equal(hardware.hardware_confirmation_required, true)
  assert.deepEqual(
    hardware.components?.filter(component => component.changed).map(component => component.name),
    ['api', 'web', 'hardware', 'updater', 'cloudflared']
  )
})

test('release notes summarize their first three content lines and retain full sections', () => {
  const notes = `## Highlights

- **Upgrade note:** Reconnect after the migration.
- Add one-click updates.
- Show changed containers before applying.
- This fourth line stays out of the summary.

## Fixed

- Preserve Hardware during Web-only updates.

**Full Changelog**: https://example.invalid/compare`

  assert.deepEqual(releaseNoteSummary(notes), [
    'Upgrade note: Reconnect after the migration.',
    'Add one-click updates.',
    'Show changed containers before applying.'
  ])
  assert.deepEqual(parseReleaseNotes(notes), [
    {
      title: 'Highlights',
      lines: [
        'Upgrade note: Reconnect after the migration.',
        'Add one-click updates.',
        'Show changed containers before applying.',
        'This fourth line stays out of the summary.'
      ]
    },
    {
      title: 'Fixed',
      lines: ['Preserve Hardware during Web-only updates.']
    }
  ])
  assert.deepEqual(releaseNoteRemainder(notes), [
    {
      title: 'Highlights',
      lines: ['This fourth line stays out of the summary.']
    },
    {
      title: 'Fixed',
      lines: ['Preserve Hardware during Web-only updates.']
    }
  ])
})

test('about panel checks automatically and applies only through the updater', async () => {
  const [panel, settingsView, notices, zhCN] = await Promise.all([
    readFile(new URL('../src/components/AboutSettingsPanel.vue', import.meta.url), 'utf8'),
    readFile(new URL('../src/views/SettingsView.vue', import.meta.url), 'utf8'),
    readFile(new URL('../../THIRD_PARTY_NOTICES.md', import.meta.url), 'utf8'),
    readFile(new URL('../src/i18n/locales/zh-CN.ts', import.meta.url), 'utf8')
  ])

  assert.match(settingsView, /id: 'about'/)
  assert.match(panel, /gateway\.checkForUpdates\(refresh\)/)
  assert.match(panel, /@click="checkForUpdates\(true\)"/)
  assert.match(panel, /:class="\{ spin: checking \}"/)
  assert.match(panel, /gateway\.applySoftwareUpdate\(/)
  assert.match(panel, /setApplicationUpdateNoticeSuppressed\(true\)/)
  assert.match(
    panel,
    /clearApplicationUpdateNotice\(\)[\s\S]*if \(operation\.state === 'succeeded'\) \{[\s\S]*refreshApplication\(\)/
  )
  assert.match(panel, /subscribeRuntimeUpdateOperations\(/)
  assert.match(panel, /monitorRuntimeUpdateOperation\(operation\.id\)/)
  assert.doesNotMatch(panel, /subscribeSoftwareUpdateEvents|getSoftwareUpdateStatus/)
  assert.doesNotMatch(panel, /operationTimer|setInterval/)
  assert.match(panel, /role="progressbar"/)
  assert.match(
    panel,
    /startingUpdate\.value[\s\S]*operation\.target_version !== currentUpdate\?\.latest_version/
  )
  assert.match(panel, /startingUpdate\.value = true[\s\S]*applying\.value = true/)
  assert.match(
    panel,
    /class="about-update__component-tag">\s*\{\{ componentLabel\(component\.name\) \}\}\s*<\/span>\s*<small[\s\S]*class="about-update__component-state"/
  )
  assert.match(
    panel,
    /\.about-update__components--progress\s*\{[^}]*grid-auto-rows:\s*28px;[^}]*align-content:\s*start;/s
  )
  assert.doesNotMatch(panel, /grid-auto-rows:\s*minmax\(0,\s*1fr\)/)
  assert.match(panel, /requestConfirmation\(/)
  assert.match(panel, /hardware_confirmation_required/)
  assert.match(panel, /releaseNoteSummary/)
  assert.match(panel, /<details/)
  assert.doesNotMatch(panel, /v-html/)
  assert.doesNotMatch(panel, /getActiveCallSnapshot|activeCalls|callState/)
  assert.match(panel, /useInitialLoadBarrier/)
  assert.match(
    panel,
    /waitForInitialLoad\(\[\(\) => load\(\), \(\) => checkForUpdates\(\)\]\)/
  )
  assert.match(panel, /<SettingsLoadBoundary[\s\S]*:loading="initialLoading"/)
  assert.doesNotMatch(panel, /<LoaderCircle/)
  assert.match(panel, /about-status--checking/)
  assert.match(panel, /about\?\.notices_url \|\| noticesURL/)
  assert.match(panel, /about\.checkAgain/)
  assert.doesNotMatch(panel, /installUpdate|downloadUpdate/)
  assert.doesNotMatch(panel, /compactCommit/)
  assert.doesNotMatch(panel, /about\?\.(?:commit|build_date)/)
  assert.match(panel, /about\.projectLicense/)
  assert.match(panel, /about\.thirdPartyNotices/)
  assert.doesNotMatch(panel, /about-version/)
  assert.match(panel, /\{\{ displayedVersion \}\}/)
  assert.match(notices, /Vue\.js, Vue Router, and Vue I18n/)
  assert.match(
    zhCN,
    /更新 Hardware 会重启 ModemManager 和设备 Agent，可能造成蜂窝硬件下线，极少数情况下需要人工重新插拔设备。确认继续？/
  )
})

test('root VERSION is the single release version for every image', async () => {
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
  assert.match(viteConfig, /readFileSync\(new URL\('\.\.\/VERSION'/)
  assert.match(viteConfig, /process\.env\.VITE_MODEMDECK_BUILD_ID/)
  assert.match(viteConfig, /fileName: 'modemdeck-build\.json'/)
  assert.match(viteConfig, /cssCodeSplit: true/)
  assert.match(dockerfile, /VITE_MODEMDECK_BUILD_ID=\$\{VERSION\}/)
  assert.doesNotMatch(dockerfile, /COPY VERSION/)
  assert.doesNotMatch(dockerfile, /main\.(?:commit|buildDate)=/)
  assert.match(dockerfile, /org\.opencontainers\.image\.revision="\$\{VCS_REF\}"/)
})
