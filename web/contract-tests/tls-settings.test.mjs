import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { gateway, setClientCSRFToken } from '../src/api/client.ts'
import {
  createTLSSettingsPayload,
  parseTLSSettingsResponse,
  tlsCAPath,
  tlsSettingsContract,
  tlsSettingsPath
} from '../src/api/contract.ts'

const canonicalTLS = {
  mode: 'user',
  subject: 'CN=modemdeck.example',
  issuer: 'CN=Example Internal CA',
  dns_names: ['modemdeck.example', 'gateway.modemdeck.example'],
  ip_addresses: ['192.0.2.10', '2001:db8::10'],
  not_before: '2026-07-01T00:00:00Z',
  not_after: '2027-07-01T00:00:00Z',
  fingerprint_sha256:
    '28:7A:58:03:5A:96:3E:CB:5A:FA:8A:71:F9:21:BF:32:58:D4:98:B5:E1:A9:81:D5:0B:7A:A0:DB:10:72:C2:09',
  expired: false,
  renews_automatically: false
}

const certificatePEM = [
  '-----BEGIN CERTIFICATE-----',
  'fixture-certificate',
  '-----END CERTIFICATE-----',
  ''
].join('\n')
const privateKeyPEM = [
  '-----BEGIN PRIVATE KEY-----',
  'fixture-private-key',
  '-----END PRIVATE KEY-----',
  ''
].join('\n')

test('TLS settings endpoint uses the fixed GET and PUT contract', () => {
  assert.equal(tlsSettingsPath, '/api/v1/settings/tls')
  assert.equal(tlsCAPath, '/api/v1/settings/tls/ca')
  assert.deepEqual(tlsSettingsContract, {
    get: {
      method: 'GET',
      path: '/api/v1/settings/tls',
      successStatus: 200
    },
    update: {
      method: 'PUT',
      path: '/api/v1/settings/tls',
      successStatus: 200
    },
    downloadCA: {
      method: 'GET',
      path: '/api/v1/settings/tls/ca',
      successStatus: 200
    }
  })
})

test('TLS settings payloads keep only fields for the selected operation', () => {
  assert.deepEqual(
    createTLSSettingsPayload({
      operation: 'install_user',
      certificate_pem: certificatePEM,
      private_key_pem: privateKeyPEM
    }),
    {
      operation: 'install_user',
      certificate_pem: certificatePEM,
      private_key_pem: privateKeyPEM
    }
  )
  assert.deepEqual(
    createTLSSettingsPayload({
      operation: 'use_automatic',
      certificate_pem: 'ignored'
    }),
    { operation: 'use_automatic' }
  )
  assert.throws(
    () =>
      createTLSSettingsPayload({
        operation: 'install_user',
        certificate_pem: ' ',
        private_key_pem: privateKeyPEM
      }),
    /certificate_pem/
  )
  assert.throws(
    () =>
      createTLSSettingsPayload({
        operation: 'install_user',
        certificate_pem: certificatePEM,
        private_key_pem: ''
      }),
    /private_key_pem/
  )
})

test('TLS settings parser requires the complete typed envelope', () => {
  assert.deepEqual(parseTLSSettingsResponse({ tls: canonicalTLS }), canonicalTLS)
  assert.throws(
    () => parseTLSSettingsResponse(canonicalTLS),
    /tls_settings_response\.tls/
  )
  assert.throws(
    () =>
      parseTLSSettingsResponse({
        tls: { ...canonicalTLS, mode: 'managed' }
      }),
    /mode/
  )
  assert.throws(
    () =>
      parseTLSSettingsResponse({
        tls: { ...canonicalTLS, dns_names: 'modemdeck.example' }
      }),
    /dns_names/
  )
  assert.throws(
    () =>
      parseTLSSettingsResponse({
        tls: { ...canonicalTLS, not_after: 'not-a-time' }
      }),
    /not_after/
  )
  assert.throws(
    () =>
      parseTLSSettingsResponse({
        tls: { ...canonicalTLS, renews_automatically: null }
      }),
    /renews_automatically/
  )
})

test('gateway sends TLS requests with the contract path, method, payload, and CSRF', async () => {
  const originalFetch = globalThis.fetch
  const requests = []
  globalThis.fetch = async (input, init = {}) => {
    requests.push({
      path: String(input),
      method: init.method,
      headers: new Headers(init.headers),
      body: init.body === undefined ? undefined : JSON.parse(String(init.body))
    })
    return new Response(JSON.stringify({ tls: canonicalTLS }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' }
    })
  }
  setClientCSRFToken('tls-test-csrf')

  try {
    assert.deepEqual(await gateway.getTLSSettings(), canonicalTLS)
    assert.deepEqual(
      await gateway.updateTLSSettings({
        operation: 'install_user',
        certificate_pem: certificatePEM,
        private_key_pem: privateKeyPEM
      }),
      canonicalTLS
    )
    assert.deepEqual(
      await gateway.updateTLSSettings({ operation: 'use_automatic' }),
      canonicalTLS
    )
  } finally {
    setClientCSRFToken()
    globalThis.fetch = originalFetch
  }

  assert.deepEqual(
    requests.map(request => [request.path, request.method]),
    [
      ['/api/v1/settings/tls', 'GET'],
      ['/api/v1/settings/tls', 'PUT'],
      ['/api/v1/settings/tls', 'PUT']
    ]
  )
  assert.equal(requests[0].headers.get('X-ModemDeck-CSRF'), null)
  assert.equal(requests[1].headers.get('X-ModemDeck-CSRF'), 'tls-test-csrf')
  assert.deepEqual(requests[1].body, {
    operation: 'install_user',
    certificate_pem: certificatePEM,
    private_key_pem: privateKeyPEM
  })
  assert.deepEqual(requests[2].body, { operation: 'use_automatic' })
})

test('admin settings group HTTPS and remote entry points under Access', async () => {
  const [
    settingsView,
    connectivityPanel,
    certificatePanel,
    originTLSPanel,
    externalAccessPanel,
    certificateFacts,
    fileDropControl,
    textareaControl,
    english,
    chinese
  ] = await Promise.all([
    readFile(new URL('../src/views/SettingsView.vue', import.meta.url), 'utf8'),
    readFile(
      new URL(
        '../src/components/ConnectivitySettingsPanel.vue',
        import.meta.url
      ),
      'utf8'
    ),
    readFile(
      new URL(
        '../src/components/WebCertificateSettingsPanel.vue',
        import.meta.url
      ),
      'utf8'
    ),
    readFile(
      new URL(
        '../src/components/CloudflareOriginTLSSettings.vue',
        import.meta.url
      ),
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
      new URL('../src/components/CertificateFacts.vue', import.meta.url),
      'utf8'
    ),
    readFile(
      new URL('../src/components/FileDropControl.vue', import.meta.url),
      'utf8'
    ),
    readFile(
      new URL('../src/components/TextareaControl.vue', import.meta.url),
      'utf8'
    ),
    readFile(new URL('../src/i18n/locales/en-US.ts', import.meta.url), 'utf8'),
    readFile(new URL('../src/i18n/locales/zh-CN.ts', import.meta.url), 'utf8')
  ])

  assert.match(settingsView, /id: 'connectivity'/)
  assert.match(settingsView, /label: t\('settings\.tls'\)/)
  assert.match(settingsView, /<ConnectivitySettingsPanel/)
  assert.match(connectivityPanel, /<WebCertificateSettingsPanel/)
  assert.match(connectivityPanel, /connectivity\.remoteAccess/)
  assert.match(connectivityPanel, /mode="modules"/)
  assert.match(
    connectivityPanel,
    /<ExternalAccessSettingsPanel mode="connectivity" :heading-level="4"/
  )
  assert.doesNotMatch(certificatePanel, /tls\.scopeNotice/)
  assert.match(certificatePanel, /HTTPS :7577/)
  assert.equal((certificatePanel.match(/<FileDropControl/g) || []).length, 2)
  assert.doesNotMatch(certificatePanel, /<input[\s\S]*?type="file"/)
  assert.match(fileDropControl, /@dragenter="handleDragEnter"/)
  assert.match(fileDropControl, /@dragover="handleDragOver"/)
  assert.match(fileDropControl, /@drop="handleDrop"/)
  assert.match(fileDropControl, /type="file"/)
  assert.equal((originTLSPanel.match(/<TextareaControl/g) || []).length, 2)
  assert.doesNotMatch(originTLSPanel, /<textarea/)
  assert.match(originTLSPanel, /<template v-if="status\.enabled">/)
  assert.match(certificatePanel, /<CertificateFacts/)
  assert.match(originTLSPanel, /<CertificateFacts/)
  assert.match(certificateFacts, /<dl class="tls-facts">/)
  assert.doesNotMatch(externalAccessPanel, /ios-origin-protocols/)
  assert.doesNotMatch(originTLSPanel, /https:\/\/modemdeck:757[56]/)
  assert.doesNotMatch(originTLSPanel, /originTLS\.tunnelSettings/)
  assert.match(originTLSPanel, /cloudflare_origin_tls_activation_failed/)
  assert.match(originTLSPanel, /<form v-else class="tls-install origin-install"/)
  assert.match(originTLSPanel, /originTLS\.delete/)
  assert.doesNotMatch(
    originTLSPanel,
    /t\(['"]originTLS\.(?:replace|replaceTitle|disable|disableTitle|disableMessage)['"]\)/
  )
  assert.match(textareaControl, /<textarea/)
  assert.match(certificatePanel, /gateway\.getTLSSettings\(\)/)
  assert.match(certificatePanel, /gateway\.updateTLSSettings\(/)
  assert.match(certificatePanel, /tlsCAPath/)
  assert.match(english, /tls: 'Access'/)
  assert.match(english, /currentCertificate: 'Local Web certificate'/)
  assert.match(chinese, /tls: '接入'/)
  assert.match(chinese, /currentCertificate: '本地 Web 证书'/)
})
