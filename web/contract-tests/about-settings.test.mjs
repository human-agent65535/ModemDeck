import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

import { gateway } from '../src/api/client.ts'

const aboutResponse = {
  name: 'ModemDeck',
  version: 'v1.0.0',
  commit: 'abc123',
  build_date: '2026-07-28T08:00:00Z',
  repository_url: 'https://github.com/human-agent65535/ModemDeck',
  license_name: 'PolyForm Noncommercial 1.0.0',
  license_url: 'https://github.com/human-agent65535/ModemDeck/blob/modemdeck/LICENSE',
  notices_url:
    'https://github.com/human-agent65535/ModemDeck/blob/modemdeck/THIRD_PARTY_NOTICES.md'
}

const updateResponse = {
  status: 'unavailable',
  current_version: 'v1.0.0',
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
  const [panel, settingsView, notices, version] = await Promise.all([
    readFile(new URL('../src/components/AboutSettingsPanel.vue', import.meta.url), 'utf8'),
    readFile(new URL('../src/views/SettingsView.vue', import.meta.url), 'utf8'),
    readFile(new URL('../../THIRD_PARTY_NOTICES.md', import.meta.url), 'utf8'),
    readFile(new URL('../../VERSION', import.meta.url), 'utf8')
  ])

  assert.match(settingsView, /id: 'about'/)
  assert.match(panel, /gateway\.checkForUpdates\(\)/)
  assert.doesNotMatch(panel, /await checkForUpdates\(\)/)
  assert.match(panel, /void load\(\)[\s\S]*void checkForUpdates\(\)/)
  assert.doesNotMatch(panel, /v-if="loading"/)
  assert.match(panel, /about-status--checking/)
  assert.match(panel, /about\?\.notices_url \|\| noticesURL/)
  assert.doesNotMatch(panel, /about\.checkAgain|installUpdate|downloadUpdate/)
  assert.match(panel, /about\.projectLicense/)
  assert.match(panel, /about\.thirdPartyNotices/)
  assert.match(notices, /Vue\.js, Vue Router, and Vue I18n/)
  assert.equal(version.trim(), '1.0.0')
})
