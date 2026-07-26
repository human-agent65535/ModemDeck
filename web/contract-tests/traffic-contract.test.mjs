import assert from 'node:assert/strict'
import test from 'node:test'
import {
  createProxyPayload,
  createProxyUpdatePayload,
  networkContracts,
  parseNetworkStatusResponse,
  parseProxyCollectionResponse,
  parseProxyMutationResponse,
  proxyDeletePath,
  proxyResourceContract
} from '../src/api/contract.ts'
import { createFixtureGateway } from '../src/api/fixture.ts'
import {
  isIPAddress,
  isLoopbackAddress,
  normalizedIPAddress
} from '../src/utils/ipAddress.ts'
import {
  proxyCredentialError,
  utf8ByteLength
} from '../src/utils/proxyCredentials.ts'
import {
  calculateNetworkRate,
  formatNetworkRate
} from '../src/utils/networkRate.ts'

const networkResponse = {
  available: true,
  state: 'available',
  boot_epoch: 'boot-1',
  observed_at: '2026-07-23T12:00:00Z',
  stale: false,
  apply_pending: false,
  apply_status: 'applied',
  apply_attempts: 0,
  apply_exhausted: false,
  lines: [
    {
      line_id: 'line-main',
      connected: true,
      interface: 'wwan0',
      addresses: ['192.0.2.10', '2001:db8::10'],
      dns: ['1.1.1.1'],
      rx_bytes: 1200,
      tx_bytes: 500,
      error: ''
    }
  ],
  proxies: [
    {
      id: 'proxy-1',
      line_id: 'line-main',
      state: 'running',
      running: true,
      mode: 'socks5',
      listen_address: '127.0.0.1',
      listen_port: 1080,
      interface: 'wwan0',
      runtime_epoch: 'runtime-1',
      started_at: '2026-07-23T12:00:00Z',
      bytes_up: 100,
      bytes_down: 300,
      connections: 4,
      active_connections: 1,
      last_error: ''
    }
  ],
  today_total: { rx_bytes: 700, tx_bytes: 200 },
  today_usage: [
    { scope_kind: 'line', scope_id: 'line-main', rx_bytes: 700, tx_bytes: 200 }
  ],
  month_total: { rx_bytes: 7000, tx_bytes: 2000 },
  month_usage: [
    { scope_kind: 'line', scope_id: 'line-main', rx_bytes: 7000, tx_bytes: 2000 }
  ]
}

const proxyResponse = {
  id: 'proxy-1',
  name: '主卡 SOCKS5 1080',
  line_id: 'line-main',
  enabled: true,
  mode: 'socks5',
  listen_address: '127.0.0.1',
  listen_port: 1080,
  auth_enabled: false,
  username: '',
  has_password: false,
  revision: 1,
  applied_revision: 1,
  apply_state: 'applied',
  created_at: '2026-07-23T12:00:00Z',
  updated_at: '2026-07-23T12:00:00Z'
}

test('network and proxy endpoints use one explicit REST contract', () => {
  assert.deepEqual(networkContracts.status, {
    method: 'GET',
    path: '/api/v1/network',
    successStatus: 200
  })
  assert.deepEqual(networkContracts.createProxy, {
    method: 'POST',
    path: '/api/v1/proxies',
    successStatus: 201
  })
  assert.deepEqual(proxyResourceContract(' proxy-1 '), {
    update: { method: 'PATCH', path: '/api/v1/proxies/proxy-1', successStatus: 200 },
    delete: { method: 'DELETE', path: '/api/v1/proxies/proxy-1', successStatus: 200 }
  })
  assert.equal(proxyDeletePath('proxy-1', 3), '/api/v1/proxies/proxy-1?revision=3')
})

test('network parser keeps line and proxy counters separate', () => {
  const parsed = parseNetworkStatusResponse(networkResponse)
  assert.equal(parsed.lines[0].interface, 'wwan0')
  assert.deepEqual(parsed.lines[0].addresses, ['192.0.2.10', '2001:db8::10'])
  assert.equal(parsed.proxies[0].state, 'running')
  assert.deepEqual(parsed.today_total, { rx_bytes: 700, tx_bytes: 200 })
  assert.equal(parsed.today_usage[0].rx_bytes, 700)
  assert.deepEqual(parsed.month_total, { rx_bytes: 7000, tx_bytes: 2000 })
  assert.equal(parsed.month_usage[0].tx_bytes, 2000)
  assert.equal(parsed.stale, false)
  assert.equal(parsed.apply_status, 'applied')
  assert.equal(parsed.apply_attempts, 0)
  assert.throws(
    () =>
      parseNetworkStatusResponse({
        ...networkResponse,
        proxies: [{ ...networkResponse.proxies[0], state: 'starting' }]
      }),
    /state/
  )
  assert.throws(
    () => parseNetworkStatusResponse({ ...networkResponse, apply_status: 'retrying' }),
    /apply_status/
  )
  assert.throws(
    () => parseNetworkStatusResponse({ ...networkResponse, apply_attempts: -1 }),
    /apply_attempts/
  )
})

test('network rate uses monotonic samples and rejects counter resets', () => {
  const previous = {
    bootEpoch: 'boot-1',
    observedAt: '2026-07-23T12:00:00Z',
    interface: 'wwan0',
    connected: true,
    rxBytes: 1_000,
    txBytes: 500
  }
  assert.deepEqual(
    calculateNetworkRate(previous, {
      ...previous,
      observedAt: '2026-07-23T12:00:05Z',
      rxBytes: 626_000,
      txBytes: 63_000
    }),
    { rxBytesPerSecond: 125_000, txBytesPerSecond: 12_500 }
  )
  assert.equal(formatNetworkRate(125_000), '1.00 Mbps')
  assert.equal(
    calculateNetworkRate(previous, {
      ...previous,
      observedAt: '2026-07-23T12:00:05Z',
      rxBytes: 100
    }),
    undefined
  )
  assert.equal(
    calculateNetworkRate(previous, {
      ...previous,
      bootEpoch: 'boot-2',
      observedAt: '2026-07-23T12:00:05Z'
    }),
    undefined
  )
})

test('proxy payloads validate listener security and revision', () => {
  assert.deepEqual(
    createProxyPayload({
      name: '主卡 SOCKS5 1080',
      line_id: 'line-main',
      enabled: true,
      mode: 'socks5',
      listen_address: '127.0.0.1',
      listen_port: 1080,
      auth_enabled: false,
      username: '',
      password: ''
    }),
    {
      revision: 0,
      name: '主卡 SOCKS5 1080',
      line_id: 'line-main',
      enabled: true,
      mode: 'socks5',
      listen_address: '127.0.0.1',
      listen_port: 1080,
      auth_enabled: false,
      username: '',
      password: ''
    }
  )
  assert.throws(
    () =>
      createProxyPayload({
        name: '外部代理',
        line_id: 'line-main',
        enabled: true,
        mode: 'http',
        listen_address: '0.0.0.0',
        listen_port: 3128,
        auth_enabled: true,
        username: 'deck',
        password: ''
      }),
    /用户名和密码/
  )
  assert.deepEqual(
    createProxyUpdatePayload({
      revision: 2,
      name: '主卡 SOCKS5 1080',
      line_id: 'line-main',
      enabled: false,
      mode: 'socks5',
      listen_address: '127.0.0.1',
      listen_port: 1080,
      auth_enabled: true,
      username: 'deck'
    }),
    {
      revision: 2,
      name: '主卡 SOCKS5 1080',
      line_id: 'line-main',
      enabled: false,
      mode: 'socks5',
      listen_address: '127.0.0.1',
      listen_port: 1080,
      auth_enabled: true,
      username: 'deck'
    }
  )
  assert.equal(
    createProxyPayload({
      name: '带空格密码',
      line_id: 'line-main',
      enabled: true,
      mode: 'http',
      listen_address: '0.0.0.0',
      listen_port: 3128,
      auth_enabled: true,
      username: 'deck',
      password: ' secret '
    }).password,
    ' secret '
  )
})

test('listener address validation accepts only real IP addresses', () => {
  assert.equal(normalizedIPAddress('127.000.0.1'), '')
  assert.equal(normalizedIPAddress(' 127.0.0.1 '), '127.0.0.1')
  assert.equal(isIPAddress('127.999.999.999'), false)
  assert.equal(isIPAddress('localhost'), false)
  assert.equal(isIPAddress('2001:db8::1'), true)
  assert.equal(isLoopbackAddress('127.12.34.56'), true)
  assert.equal(isLoopbackAddress('::1'), true)
  assert.equal(isLoopbackAddress('2001:db8::1'), false)
})

test('proxy credential limits use UTF-8 bytes and protocol rules', () => {
  assert.equal(utf8ByteLength('密'), 3)
  assert.equal(proxyCredentialError('socks5', '用'.repeat(42), 'secret'), '')
  assert.match(
    proxyCredentialError('socks5', '用'.repeat(43), 'secret'),
    /128 个 UTF-8 字节/
  )
  assert.equal(proxyCredentialError('socks5', 'deck', '密'.repeat(85)), '')
  assert.match(
    proxyCredentialError('socks5', 'deck', '密'.repeat(86)),
    /255 个 UTF-8 字节/
  )
  assert.equal(proxyCredentialError('http', 'deck', 'p'.repeat(512)), '')
  assert.match(
    proxyCredentialError('http', 'deck', 'p'.repeat(513)),
    /512 个 UTF-8 字节/
  )
  assert.match(
    proxyCredentialError('http', 'invalid:user', 'secret'),
    /不能包含冒号/
  )
})

test('proxy responses never require or expose a password', () => {
  const collection = parseProxyCollectionResponse({ proxies: [proxyResponse] })
  const mutation = parseProxyMutationResponse({
    proxy: {
      ...proxyResponse,
      auth_enabled: true,
      username: 'deck',
      has_password: true,
      revision: 2,
      applied_revision: 1,
      apply_state: 'pending_update'
    },
    applied: false,
    status: 'pending'
  })
  assert.equal(collection[0].has_password, false)
  assert.equal(collection[0].applied_revision, 1)
  assert.equal(collection[0].apply_state, 'applied')
  assert.equal('password' in collection[0], false)
  assert.equal(mutation.proxy.has_password, true)
  assert.equal(mutation.proxy.apply_state, 'pending_update')
  assert.equal(mutation.status, 'pending')
  assert.equal('password' in mutation.proxy, false)
  assert.throws(
    () =>
      parseProxyCollectionResponse({
        proxies: [{ ...proxyResponse, apply_state: 'retrying' }]
      }),
    /apply_state/
  )
  assert.throws(
    () =>
      parseProxyCollectionResponse({
        proxies: [{ ...proxyResponse, applied_revision: 2 }]
      }),
    /applied_revision/
  )
})

test('fixture supports proxy CRUD, waiting bearer, and write-only passwords', async () => {
  const gateway = createFixtureGateway()
  const initial = await gateway.getNetworkStatus()
  assert.equal(initial.lines[0].connected, true)
  assert.equal(initial.proxies[0].state, 'running')
  assert.equal(initial.proxies[1].state, 'waiting_for_bearer')

  await assert.rejects(
    gateway.createProxy({
      name: '无效监听地址',
      line_id: 'line-fixture-main',
      enabled: true,
      mode: 'http',
      listen_address: '127.999.999.999',
      listen_port: 8080,
      auth_enabled: false,
      username: '',
      password: ''
    }),
    /IPv4 或 IPv6/
  )

  const created = await gateway.createProxy({
    name: '主卡 HTTP 8080',
    line_id: 'line-fixture-main',
    enabled: true,
    mode: 'http',
    listen_address: '0.0.0.0',
    listen_port: 8080,
    auth_enabled: true,
    username: 'deck',
    password: 'secret'
  })
  assert.equal(created.proxy.has_password, true)
  assert.equal(created.proxy.applied_revision, created.proxy.revision)
  assert.equal(created.proxy.apply_state, 'applied')
  assert.equal('password' in created.proxy, false)

  const updated = await gateway.updateProxy(created.proxy.id, {
    revision: created.proxy.revision,
    name: created.proxy.name,
    line_id: created.proxy.line_id,
    enabled: false,
    mode: created.proxy.mode,
    listen_address: created.proxy.listen_address,
    listen_port: created.proxy.listen_port,
    auth_enabled: true,
    username: 'deck'
  })
  assert.equal(updated.proxy.enabled, false)
  assert.equal(updated.proxy.has_password, true)
  assert.equal(updated.proxy.applied_revision, updated.proxy.revision)

  await gateway.deleteProxy(updated.proxy.id, updated.proxy.revision)
  assert.equal((await gateway.listProxies()).some(item => item.id === updated.proxy.id), false)
})
