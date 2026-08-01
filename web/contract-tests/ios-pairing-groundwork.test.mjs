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

test('pairing exposes only verified selectable API addresses', () => {
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
      paired: true,
      paired_at: '2026-07-30T12:01:00Z',
      server_urls: [
        'https://phone-a.example.com',
        'https://phone-b.example.com'
      ],
      credential_created_at: '2026-07-30T12:00:00Z'
    }
  })
  assert.equal(status.pairing.availability, 'ready')
  assert.equal(status.pairing.has_credential, true)
  assert.equal(status.pairing.paired, true)
  assert.deepEqual(status.pairing.server_urls, [
    'https://phone-a.example.com',
    'https://phone-b.example.com'
  ])
  assert.equal('cloudflare' in status.pairing, false)
  assert.equal('turn' in status.pairing, false)
  assert.equal(status.payload, undefined)

  assert.deepEqual(externalAccessContract, {
    getStatus: {
      method: 'GET',
      path: '/api/v1/external-access/status',
      successStatus: 200
    },
    refresh: {
      method: 'POST',
      path: '/api/v1/external-access/refresh',
      successStatus: 200
    },
    installOriginTLS: {
      method: 'PUT',
      path: '/api/v1/external-access/origin-tls',
      successStatus: 200
    },
    disableOriginTLS: {
      method: 'DELETE',
      path: '/api/v1/external-access/origin-tls',
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
      verified_api_urls: ['https://phone.example.com'],
      web_urls: ['https://deck.example.com'],
      origin_routes: [
        {
          kind: 'api',
          public_url: 'https://phone.example.com',
          service_url: 'https://modemdeck:7575',
          https: true,
          http2: true,
          tls_name_configured: true,
          tls_verification: true
        }
      ]
    },
    turn: {
      configured: true,
      available: true
    },
    origin_tls: {
      enabled: false,
      covers_routes: false,
      subject: '',
      issuer: '',
      dns_names: [],
      not_before: '',
      not_after: '',
      fingerprint_sha256: '',
      expired: false
    }
  })
  assert.equal(external.cloudflare.public_url, 'https://phone.example.com')
  assert.deepEqual(external.cloudflare.origin_routes, [
    {
      kind: 'api',
      public_url: 'https://phone.example.com',
      service_url: 'https://modemdeck:7575',
      https: true,
      http2: true,
      tls_name_configured: true,
      tls_verification: true
    }
  ])
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
    verified_api_urls: ['https://mobile.modemdeck.example'],
    web_urls: ['https://web.modemdeck.example'],
    origin_routes: [
      {
        kind: 'api',
        public_url: 'https://mobile.modemdeck.example',
        service_url: 'http://modemdeck:7575',
        https: false,
        http2: false,
        tls_name_configured: false,
        tls_verification: true
      },
      {
        kind: 'web',
        public_url: 'https://web.modemdeck.example',
        service_url: 'http://modemdeck:7576',
        https: false,
        http2: false,
        tls_name_configured: false,
        tls_verification: true
      }
    ]
  })

  const created = await gateway.createIOSPairing()
  assert.equal(created.pairing.has_credential, true)
  assert.equal(created.pairing.paired, false)
  assert.equal(created.payload?.server_url, 'https://mobile.modemdeck.example')
  assert.ok(created.payload?.token)

  await gateway.revokeIOSPairing()
  assert.equal((await gateway.getIOSPairing()).pairing.has_credential, false)
  assert.equal((await gateway.getIOSPairing()).pairing.paired, false)
})

test('fixture can preview a missing Origin SNI diagnosis', async () => {
  const gateway = createFixtureGateway({ externalAccessDiagnostic: 'origin-sni' })
  const status = await gateway.getExternalAccessStatus()

  assert.equal(status.cloudflare.connector_connected, true)
  assert.equal(status.cloudflare.connected, false)
  assert.equal(status.origin_tls.enabled, true)
  assert.equal(status.cloudflare.origin_routes.length, 2)
  assert.equal(
    status.cloudflare.origin_routes.every(route =>
      route.https && route.http2 && !route.tls_name_configured
    ),
    true
  )
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

test('settings separate administrator infrastructure from self-service pairing', async () => {
  const [settingsView, settingsNavigation, userPanel, pairingPanel, connectivityPanel, externalAccessPanel, callMedia] =
    await Promise.all([
    readFile(new URL('../src/views/SettingsView.vue', import.meta.url), 'utf8'),
    readFile(
      new URL('../src/components/settings/settingsNavigation.ts', import.meta.url),
      'utf8'
    ),
    readFile(
      new URL('../src/components/UserSettingsPanel.vue', import.meta.url),
      'utf8'
    ),
    readFile(
      new URL('../src/components/PairingSettingsPanel.vue', import.meta.url),
      'utf8'
    ),
    readFile(
      new URL('../src/components/ConnectivitySettingsPanel.vue', import.meta.url),
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

  assert.match(settingsView, /id: 'pairing'/)
  assert.match(settingsView, /id: 'connectivity'/)
  assert.match(settingsView, /visibleSettingsSectionIDs\(/)
  assert.match(
    settingsNavigation,
    /if \(section === 'pairing'\) return visibility\.canPairIOS/
  )
  assert.match(settingsNavigation, /'connectivity'/)
  assert.match(settingsNavigation, /if \(ADMIN_ONLY_SECTIONS\.has\(section\)\)/)
  assert.match(settingsView, /sessionState\.iosPairingEnabled/)
  assert.doesNotMatch(settingsView, /externalAccessEnabled|loadExternalAccessVisibility/)
  assert.match(settingsView, /<PairingSettingsPanel/)
  assert.match(settingsView, /<ConnectivitySettingsPanel/)
  assert.match(pairingPanel, /<ExternalAccessSettingsPanel mode="pairing"/)
  assert.match(connectivityPanel, /<WebCertificateSettingsPanel/)
  assert.match(connectivityPanel, /<ExternalAccessSettingsPanel mode="connectivity"/)
  assert.match(userPanel, /ios_pairing_enabled: iosPairingEnabled\.value/)
  assert.match(userPanel, /selectedUser\.ios_pairing_has_credential/)
  assert.match(userPanel, /selectedUser\.ios_pairing_paired/)
  assert.match(userPanel, /gateway\.revokeUserIOSPairing\(user\.id\)/)
  assert.match(externalAccessPanel, /gateway\.getExternalAccessStatus\(\)/)
  assert.match(externalAccessPanel, /gateway\.refreshExternalAccess\(\)/)
  assert.match(externalAccessPanel, /showConnectivity && externalAccess/)
  assert.match(externalAccessPanel, /externalAccess\.cloudflare\.api_urls/)
  assert.match(externalAccessPanel, /externalAccess\.cloudflare\.web_urls/)
  assert.match(externalAccessPanel, /cloudflare\.origin_routes/)
  assert.match(externalAccessPanel, /originHTTPSRequired/)
  assert.match(externalAccessPanel, /originSNIRequired/)
  assert.match(externalAccessPanel, /originHTTP2Recommended/)
  assert.match(externalAccessPanel, /originTLSVerificationDisabled/)
  assert.match(externalAccessPanel, /tunnelPublicVerificationFailed/)
  assert.doesNotMatch(externalAccessPanel, /verified_api_urls|ios-route/)
  assert.match(
    externalAccessPanel,
    /'is-active': externalAccess\.cloudflare\.connected/
  )
  assert.match(
    externalAccessPanel,
    /<div class="ios-tunnel-status">[\s\S]*?ios-refresh-button[\s\S]*?<span[\s\S]*?class="ios-status"/
  )
  assert.match(externalAccessPanel, /externalAccess\.turn\.configured/)
  assert.match(externalAccessPanel, /externalAccess\.turn\.available/)
  assert.match(externalAccessPanel, /pairing\.value\.availability === 'ready'/)
  assert.match(externalAccessPanel, /turnCallUnavailable/)
  assert.doesNotMatch(externalAccessPanel, /turnReady/)
  assert.match(externalAccessPanel, /window\.setInterval/)
  assert.match(
    externalAccessPanel,
    /PAIRING_CONFIRMATION_INTERVAL_MS = 2_000/
  )
  assert.match(externalAccessPanel, /refreshPairingConfirmation/)
  assert.match(
    externalAccessPanel,
    /!wasPaired && status\.paired && qrDataURL\.value/
  )
  assert.match(
    externalAccessPanel,
    /pairing\.value\?\.has_credential && !pairing\.value\.paired/
  )
  assert.match(externalAccessPanel, /onBeforeUnmount/)
  assert.match(externalAccessPanel, /gateway\.createIOSPairing\(/)
  assert.match(externalAccessPanel, /pairing\.server_urls\.length > 1/)
  assert.match(
    externalAccessPanel,
    /<SelectControl[\s\S]*:model-value="selectedServerURL"/
  )
  assert.match(externalAccessPanel, /gateway\.revokeIOSPairing\(\)/)
  assert.match(
    externalAccessPanel,
    /pairingCode\.value = JSON\.stringify\(result\.payload\)/
  )
  assert.match(externalAccessPanel, /QRCode\.toDataURL\(pairingCode\.value/)
  assert.match(
    externalAccessPanel,
    /navigator\.clipboard\.writeText\(pairingCode\.value\)/
  )
  assert.match(externalAccessPanel, /document\.execCommand\('copy'\)/)
  assert.match(
    externalAccessPanel,
    /ref="pairingCodeInput"[\s\S]*?readonly/
  )
  assert.match(externalAccessPanel, /iosPairing\.sameDeviceHint/)
  assert.match(callMedia, /gateway\.getCallMediaICEConfiguration/)
  assert.match(callMedia, /iceTransportPolicy/)
  assert.doesNotMatch(
    externalAccessPanel,
    /getMobileSettings|updateMobileSettings|public_api_url|<input|localStorage|expires_at|local[_A-Z-]?network/i
  )
})
