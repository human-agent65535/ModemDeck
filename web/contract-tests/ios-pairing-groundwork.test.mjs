import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import {
  iosPairingContract,
  parseIOSPairingResponse
} from '../src/api/contract.ts'
import { createFixtureGateway } from '../src/api/fixture.ts'

test('pairing uses the installation-managed Cloudflare endpoint', () => {
  assert.deepEqual(iosPairingContract, {
    get: {
      method: 'GET',
      path: '/api/v1/mobile/pairing',
      successStatus: 200
    },
    create: {
      method: 'POST',
      path: '/api/v1/mobile/pairing',
      successStatus: 201
    },
    revoke: {
      method: 'DELETE',
      path: '/api/v1/mobile/pairing',
      successStatus: 204
    }
  })

  const status = parseIOSPairingResponse({
    pairing: {
      allowed: true,
      cloudflare: {
        enabled: true,
        connected: true,
        public_url: 'https://phone.example.com'
      },
      has_credential: true,
      credential_created_at: '2026-07-30T12:00:00Z'
    }
  })
  assert.equal(status.pairing.cloudflare.public_url, 'https://phone.example.com')
  assert.equal(status.pairing.cloudflare.connected, true)
  assert.equal(status.payload, undefined)

  const created = parseIOSPairingResponse({
    pairing: status.pairing,
    payload: {
      version: 1,
      type: 'modemdeck.ios.pairing',
      server_url: 'https://phone.example.com',
      token: 'md_ios_secret'
    }
  })
  assert.equal(created.payload?.server_url, 'https://phone.example.com')
  assert.equal(created.payload?.token, 'md_ios_secret')
  assert.equal('expires_at' in created.payload, false)
})

test('fixture creates and revokes one non-expiring Cloudflare pairing', async () => {
  const gateway = createFixtureGateway()
  const initial = await gateway.getIOSPairing()
  assert.deepEqual(initial.pairing.cloudflare, {
    enabled: true,
    connected: true,
    public_url: 'https://mobile.modemdeck.example'
  })

  const created = await gateway.createIOSPairing()
  assert.equal(created.pairing.has_credential, true)
  assert.equal(
    created.payload?.server_url,
    created.pairing.cloudflare.public_url
  )
  assert.ok(created.payload?.token)

  await gateway.revokeIOSPairing()
  assert.equal((await gateway.getIOSPairing()).pairing.has_credential, false)
})

test('settings UI exposes read-only Cloudflare status and self-service pairing', async () => {
  const [settingsView, userPanel, iosPanel] = await Promise.all([
    readFile(new URL('../src/views/SettingsView.vue', import.meta.url), 'utf8'),
    readFile(
      new URL('../src/components/UserSettingsPanel.vue', import.meta.url),
      'utf8'
    ),
    readFile(
      new URL('../src/components/IOSAppSettingsPanel.vue', import.meta.url),
      'utf8'
    )
  ])

  assert.match(settingsView, /id: 'ios' as const/)
  assert.match(settingsView, /<IOSAppSettingsPanel/)
  assert.doesNotMatch(settingsView, /TLSSettingsForm|selectedSection === 'tls'/)
  assert.match(userPanel, /ios_pairing_enabled: iosPairingEnabled\.value/)
  assert.match(iosPanel, /pairing\.value\.cloudflare\.enabled/)
  assert.match(iosPanel, /pairing\.value\.cloudflare\.connected/)
  assert.match(iosPanel, /http:\/\/modemdeck:7575/)
  assert.match(iosPanel, /http:\/\/api:8080/)
  assert.match(iosPanel, /gateway\.createIOSPairing\(\)/)
  assert.match(iosPanel, /gateway\.revokeIOSPairing\(\)/)
  assert.match(iosPanel, /QRCode\.toDataURL/)
  assert.doesNotMatch(
    iosPanel,
    /getMobileSettings|updateMobileSettings|public_api_url|<input|localStorage|expires_at|local[_A-Z-]?network/i
  )
})
