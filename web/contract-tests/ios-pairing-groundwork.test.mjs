import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import {
  callMediaICEContract,
  createCallMediaICEPayload,
  externalAccessContract,
  iosPairingContract,
  parseCallMediaICEConfiguration,
  parseExternalAccessStatusResponse,
  parseIOSPairingResponse
} from '../src/api/contract.ts'
import { createFixtureGateway } from '../src/api/fixture.ts'

test('pairing keeps infrastructure status behind the administrator endpoint', () => {
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
      availability: 'ready',
      has_credential: true,
      credential_created_at: '2026-07-30T12:00:00Z'
    }
  })
  assert.equal(status.pairing.availability, 'ready')
  assert.equal(status.pairing.has_credential, true)
  assert.equal('cloudflare' in status.pairing, false)
  assert.equal('turn' in status.pairing, false)
  assert.equal(status.payload, undefined)

  assert.deepEqual(externalAccessContract, {
    getStatus: {
      method: 'GET',
      path: '/api/v1/external-access/status',
      successStatus: 200
    }
  })
  const external = parseExternalAccessStatusResponse({
    cloudflare: {
      enabled: true,
      connector_connected: true,
      connected: true,
      public_url: 'https://phone.example.com',
      api_urls: ['https://phone.example.com'],
      web_urls: ['https://deck.example.com']
    },
    turn: {
      configured: true,
      available: true
    }
  })
  assert.equal(external.cloudflare.public_url, 'https://phone.example.com')
  assert.deepEqual(external.turn, {
    configured: true,
    available: true
  })

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
  assert.equal(initial.pairing.availability, 'ready')
  assert.deepEqual((await gateway.getExternalAccessStatus()).cloudflare, {
    enabled: true,
    connector_connected: true,
    connected: true,
    public_url: 'https://mobile.modemdeck.example',
    api_urls: ['https://mobile.modemdeck.example'],
    web_urls: ['https://web.modemdeck.example']
  })

  const created = await gateway.createIOSPairing()
  assert.equal(created.pairing.has_credential, true)
  assert.equal(created.payload?.server_url, 'https://mobile.modemdeck.example')
  assert.ok(created.payload?.token)

  await gateway.revokeIOSPairing()
  assert.equal((await gateway.getIOSPairing()).pairing.has_credential, false)
})

test('Cloudflare Web call media accepts relay-only ICE configuration', () => {
  assert.deepEqual(callMediaICEContract('call-1'), {
    method: 'POST',
    path: '/api/v1/calls/call-1/media/ice',
    successStatus: 200
  })
  assert.deepEqual(createCallMediaICEPayload('browser-1'), {
    holder_id: 'browser-1'
  })
  assert.deepEqual(
    parseCallMediaICEConfiguration({
      ice_servers: [
        {
          urls: ['turns:turn.example.test:443?transport=tcp'],
          username: 'relay-user',
          credential: 'relay-credential'
        }
      ],
      ice_transport_policy: 'relay',
      expires_at: '2026-08-01T00:00:00Z'
    }),
    {
      ice_servers: [
        {
          urls: ['turns:turn.example.test:443?transport=tcp'],
          username: 'relay-user',
          credential: 'relay-credential'
        }
      ],
      ice_transport_policy: 'relay',
      expires_at: '2026-08-01T00:00:00Z'
    }
  )
})

test('settings expose capability-scoped infrastructure and self-service pairing', async () => {
  const [settingsView, userPanel, externalAccessPanel, callMedia] =
    await Promise.all([
    readFile(new URL('../src/views/SettingsView.vue', import.meta.url), 'utf8'),
    readFile(
      new URL('../src/components/UserSettingsPanel.vue', import.meta.url),
      'utf8'
    ),
    readFile(
      new URL(
        '../src/components/ExternalAccessSettingsPanel.vue',
        import.meta.url
      ),
      'utf8'
    ),
    readFile(
      new URL('../src/state/callMedia.ts', import.meta.url),
      'utf8'
    )
  ])

  assert.match(settingsView, /id: 'external-access'/)
  assert.match(
    settingsView,
    /canViewExternalAccess[\s\S]*canManageExternalAccess\.value \|\| canPairIOS\.value/
  )
  assert.match(settingsView, /sessionState\.iosPairingEnabled/)
  assert.doesNotMatch(settingsView, /externalAccessEnabled|loadExternalAccessVisibility/)
  assert.match(settingsView, /<ExternalAccessSettingsPanel/)
  assert.match(settingsView, /id: 'web-certificate'/)
  assert.match(settingsView, /<WebCertificateSettingsPanel/)
  assert.match(userPanel, /ios_pairing_enabled: iosPairingEnabled\.value/)
  assert.match(userPanel, /selectedUser\.ios_pairing_has_credential/)
  assert.match(userPanel, /gateway\.revokeUserIOSPairing\(user\.id\)/)
  assert.match(externalAccessPanel, /gateway\.getExternalAccessStatus\(\)/)
  assert.match(externalAccessPanel, /isAdmin && externalAccess/)
  assert.match(externalAccessPanel, /externalAccess\.cloudflare\.api_urls/)
  assert.match(externalAccessPanel, /externalAccess\.cloudflare\.web_urls/)
  assert.match(
    externalAccessPanel,
    /'is-active': externalAccess\.cloudflare\.connector_connected/
  )
  assert.match(externalAccessPanel, /externalAccess\.turn\.configured/)
  assert.match(externalAccessPanel, /externalAccess\.turn\.available/)
  assert.match(externalAccessPanel, /pairing\.value\.availability === 'ready'/)
  assert.match(externalAccessPanel, /turnCallUnavailable/)
  assert.doesNotMatch(externalAccessPanel, /turnReady/)
  assert.match(externalAccessPanel, /window\.setInterval/)
  assert.match(externalAccessPanel, /onBeforeUnmount/)
  assert.match(externalAccessPanel, /gateway\.createIOSPairing\(\)/)
  assert.match(externalAccessPanel, /gateway\.revokeIOSPairing\(\)/)
  assert.match(externalAccessPanel, /QRCode\.toDataURL/)
  assert.match(callMedia, /gateway\.getCallMediaICEConfiguration/)
  assert.match(callMedia, /iceTransportPolicy/)
  assert.doesNotMatch(
    externalAccessPanel,
    /getMobileSettings|updateMobileSettings|public_api_url|<input|localStorage|expires_at|local[_A-Z-]?network/i
  )
})
