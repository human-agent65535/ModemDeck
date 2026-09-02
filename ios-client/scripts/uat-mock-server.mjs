import { readFile } from 'node:fs/promises'
import https from 'node:https'

function argument(name, fallback = '') {
  const index = process.argv.indexOf(name)
  return index >= 0 ? process.argv[index + 1] || '' : fallback
}

const certificatePath = argument('--cert')
const keyPath = argument('--key')
const port = Number(argument('--port', '8443'))
const token = process.env.MODEMDECK_UAT_TOKEN || ''
const mutable = process.argv.includes('--mutable')
let online = true

// 本地 UAT fixture 仅使用文档保留号码与合成标识，不包含真实个人信息。

if (!certificatePath || !keyPath || !Number.isSafeInteger(port) || port < 1 || port > 65535) {
  throw new Error('Usage: node scripts/uat-mock-server.mjs --cert CERT --key KEY [--port 8443]')
}
if (!/^md_ios_[A-Za-z0-9_-]{43}$/.test(token)) {
  throw new Error('MODEMDECK_UAT_TOKEN must be a valid disposable iOS pairing token')
}

const pageMeta = { limit: 50, next_cursor: '', has_more: false }
const bootstrap = {
  capabilities: {
    agent_connected: true,
    dial: true,
    message: true,
    webrtc_audio: true,
    device_control: false,
    volte_control: false,
    vowifi_control: false,
    unavailable_reasons: {}
  },
  lines: [
    {
      id: 'uat-line-a',
      phone_number: '+1 202 555 0100',
      operator: 'Example Mobile',
      serving_operator_name: 'Example Mobile',
      registration_state: 'registered',
      roaming: false,
      device_name: 'UAT modem',
      line_label: 'Line A',
      line_color: 'teal',
      state: 'registered',
      signal_quality: 82,
      capabilities: {
        modem: true,
        sim: true,
        voice: true,
        messaging: true,
        media: true,
        dial: true,
        answer_call: true,
        reject_call: true,
        hangup_call: true,
        send_dtmf: true,
        send_message: true
      }
    }
  ],
  line_catalog: [],
  line_settings: { default_line_id: 'uat-line-a', revision: 1 },
  system_settings: { language: 'zh-CN', revision: 1 }
}
bootstrap.line_catalog = bootstrap.lines

const contacts = [
  {
    id: 'uat-contact-example',
    display_name: '示例联系人',
    avatar: '',
    notes: '仅用于本机界面测试',
    preferred_line_id: 'uat-line-a',
    favorite: true,
    revision: 1,
    created_at: '2026-08-09T08:00:00Z',
    updated_at: '2026-08-09T08:00:00Z',
    phones: [
      {
        id: 'uat-phone-example',
        label: 'mobile',
        original_number: '+1 202 555 0101',
        canonical_e164: '+12025550101',
        region: 'US',
        primary: true
      }
    ]
  }
]

const threads = [
  {
    key: 'uat-line-a:+12025550101',
    line_id: 'uat-line-a',
    peer: '+1 202 555 0101',
    contact_id: 'uat-contact-example',
    contact_name: '示例联系人',
    last_message_id: 30,
    last_timestamp: '2026-08-09T08:32:00Z',
    last_content: '第三条未读 UAT 消息。',
    unread_count: 3,
    marked_unread: false,
    favorite: true
  },
  {
    key: 'uat-line-a:UAT-SERVICE',
    line_id: 'uat-line-a',
    peer: 'UAT-SERVICE',
    last_message_id: 31,
    last_timestamp: '2026-08-09T08:33:00Z',
    last_content: '未绑定发送方 UAT 消息。',
    unread_count: 0,
    marked_unread: false,
    favorite: false
  }
]

const messages = [
  ...Array.from({ length: 27 }, (_, index) => {
    const incoming = index % 3 !== 0
    return {
      id: index + 1,
      line_id: 'uat-line-a',
      peer: '+1 202 555 0101',
      direction: incoming ? 'incoming' : 'outgoing',
      content: incoming
        ? `历史 UAT 消息 ${index + 1}`
        : `历史 UAT 回复 ${index + 1}`,
      timestamp: new Date(Date.UTC(2026, 7, 9, 8, index)).toISOString(),
      type: incoming ? 1 : 2,
      status: 0,
      revision: 1
    }
  }),
  ...[28, 29, 30].map(id => ({
    id,
    line_id: 'uat-line-a',
    peer: '+1 202 555 0101',
    direction: 'incoming',
    content: `第 ${id - 27} 条未读 UAT 消息。`,
    timestamp: `2026-08-09T08:${id + 2}:00Z`,
    type: 1,
    status: 0,
    revision: 1
  }))
]

const calls = [
  {
    id: 'uat-call-example',
    line_id: 'uat-line-a',
    direction: 'incoming',
    remote_number: '+1 202 555 0101',
    contact_id: 'uat-contact-example',
    contact_name: '示例联系人',
    started_at: '2026-08-09T07:45:00Z',
    ended_at: '2026-08-09T07:47:05Z',
    duration_seconds: 125,
    missed: false,
    read: true,
    favorite: false
  },
  {
    id: 'uat-call-missed', line_id: 'uat-line-a', direction: 'incoming',
    remote_number: '+1 202 555 0103', started_at: '2026-08-09T07:40:00Z',
    ended_at: '2026-08-09T07:40:30Z', duration_seconds: 0,
    missed: true, read: false, favorite: false
  }
]

const recordings = [
  {
    segment: {
      id: 'uat-recording-example',
      call_id: 'uat-call-example',
      segment_index: 1,
      status: 'ready',
      created_at: '2026-08-09T07:45:00Z',
      started_at: '2026-08-09T07:45:03Z',
      ended_at: '2026-08-09T07:47:05Z',
      duration_ms: 122000,
      size_bytes: 48000
    },
    call: {
      id: 'uat-call-example',
      line_id: 'uat-line-a',
      direction: 'incoming',
      remote_number: '+1 202 555 0101',
      contact_id: 'uat-contact-example',
      contact_name: '示例联系人',
      started_at: '2026-08-09T07:45:00Z',
      ended_at: '2026-08-09T07:47:05Z',
      duration_seconds: 125,
      missed: false
    },
    playable: true,
    favorite: false
  }
]

const devices = [
  {
    imei: '000000000000001',
    endpoint_id: 'uat-endpoint-a',
    name: 'UAT modem',
    model: 'Example LTE Modem',
    firmware: 'UAT-1.0',
    port: '/dev/uat-modem',
    public_ip: '',
    private_ip: '',
    public_ipv6: '',
    private_ipv6: '',
    current_iccid: '8900000000000000001',
    sim_inserted: true,
    signal_quality: 82,
    signal_dbm: -75,
    signal_rsrq: -9,
    signal_rsrp: -96,
    last_seen: '2026-08-09T08:20:00Z',
    present: true,
    created_at: '2026-08-09T07:00:00Z',
    updated_at: '2026-08-09T08:20:00Z'
  }
]

const users = [
  {
    id: 'ios-uat-user',
    username: 'ios-uat-admin',
    role: 'admin',
    enabled: true,
    ios_pairing_enabled: true,
    ios_pairing_has_credential: true,
    ios_pairing_paired: true,
    ios_pairing_device_count: 2,
    ios_pairing_pending: false,
    profile_name: 'UAT Administrator',
    line_ids: ['uat-line-a'],
    revision: 1,
    created_at: '2026-08-09T07:00:00Z',
    updated_at: '2026-08-09T08:20:00Z'
  },
  {
    id: 'ios-uat-member',
    username: 'ios-uat-member',
    role: 'member',
    enabled: true,
    ios_pairing_enabled: true,
    ios_pairing_has_credential: true,
    ios_pairing_paired: true,
    ios_pairing_device_count: 1,
    ios_pairing_pending: false,
    profile_name: 'UAT Member',
    line_ids: ['uat-line-a'],
    revision: 1,
    created_at: '2026-08-09T07:10:00Z',
    updated_at: '2026-08-09T08:20:00Z'
  }
]

const telegramUnits = [
  {
    id: 'uat-telegram',
    display_name: 'UAT notifications',
    enabled: true,
    chat_id: '-1000000000000',
    admin_id: '100000000',
    assigned_user_id: 'ios-uat-user',
    assigned_username: 'ios-uat-admin',
    all_assigned_lines: true,
    effective_enabled: true,
    line_scopes: [],
    incoming_sms: true,
    missed_calls: true,
    token_configured: true,
    token_hint: '0000',
    bot_username: 'modemdeck_uat_bot',
    verified_at: '2026-08-09T08:00:00Z',
    last_error_class: '',
    revision: 1,
    created_at: '2026-08-09T07:00:00Z',
    updated_at: '2026-08-09T08:20:00Z'
  }
]

function send(response, status, body, contentType = 'application/json; charset=utf-8') {
  const payload = body === undefined ? '' : JSON.stringify(body)
  response.writeHead(status, {
    'Cache-Control': 'no-store',
    'Content-Type': contentType,
    'Content-Length': Buffer.byteLength(payload)
  })
  response.end(payload)
}

function authorize(request, response) {
  if (request.headers.authorization === `Bearer ${token}`) return true
  send(response, 401, {
    code: 'authentication_required',
    message: 'Authentication is required'
  })
  return false
}

function readOnlyBody(pathname) {
  if (/^\/api\/v1\/calls\/[^/]+\/recordings$/.test(pathname)) {
    const callID = decodeURIComponent(pathname.split('/')[4] || '')
    return {
      state: {
        call_id: callID,
        enabled: false,
        status: 'off',
        active_segment_id: null,
        last_error_code: null
      },
      segments: []
    }
  }
  switch (pathname) {
  case '/api/v1/mobile/session':
    return {
      authenticated: true,
      setup_required: false,
      user_id: 'ios-uat-user',
      username: 'ios-uat-admin',
      role: 'admin',
      profile_contact_id: 'uat-contact-example',
      ios_pairing_enabled: true,
      language: 'zh-CN',
      allowed_line_ids: ['uat-line-a']
    }
  case '/api/v1/bootstrap':
    return bootstrap
  case '/api/v1/contacts':
    return { contacts, meta: pageMeta }
  case '/api/v1/messages/threads':
    return { threads, meta: pageMeta }
  case '/api/v1/messages':
    return { messages, meta: pageMeta }
  case '/api/v1/calls':
    return { calls, meta: pageMeta }
  case '/api/v1/calls/active':
    return { calls: [], reservations: [] }
  case '/api/v1/recordings':
    return { recordings, meta: pageMeta }
  case '/api/v1/devices':
    return { devices }
  case '/api/v1/account/sessions':
    return {
      sessions: [
        {
          id: 'ios-pairing-uat-phone',
          kind: 'ios',
          created_at: '2026-08-09T07:00:00Z',
          paired_at: '2026-08-09T07:01:00Z',
          last_seen_at: '2026-08-09T08:20:00Z',
          current: true,
          paired: true,
          device: {
            device_name: 'UAT iPhone',
            device_model: 'iPhone',
            device_model_identifier: 'iPhone-UAT',
            os_name: 'iOS',
            os_version: '27.0',
            app_version: '0.1.0',
            app_build: '7'
          }
        },
        {
          id: 'ios-pairing-uat-tablet',
          kind: 'ios',
          created_at: '2026-08-09T07:05:00Z',
          paired_at: '2026-08-09T07:06:00Z',
          last_seen_at: '2026-08-09T08:19:00Z',
          current: false,
          paired: true,
          device: {
            device_name: 'UAT iPad',
            device_model: 'iPad',
            device_model_identifier: 'iPad-UAT',
            os_name: 'iPadOS',
            os_version: '27.0',
            app_version: '0.1.0',
            app_build: '7'
          }
        }
      ]
    }
  case '/api/v1/users':
    return { users }
  case '/api/v1/settings/telegram':
    return { units: telegramUnits }
  case '/api/v1/settings/calls':
    return { receive_calls: true, revision: 1 }
  case '/api/v1/settings/recording':
    return { settings: { default_enabled: false, revision: 1 } }
  case '/api/v1/diagnostics':
    return {
      status: 'ok',
      observed_at: '2026-08-09T08:20:00Z',
      database: { available: true },
      host_agent: {
        connected: true,
        provider: 'UAT hardware service',
        agent_version: 'UAT-1.0',
        runtime_version: 'UAT-runtime',
        boot_epoch: 'uat-boot',
        revision: 'uat-revision',
        observed_at: '2026-08-09T08:20:00Z',
        capabilities: {}
      },
      call_runtime: { available: true },
      lines: bootstrap.lines,
      active_calls: []
    }
  default:
    return undefined
  }
}

// Mutable mode is explicitly opt-in and only edits these in-memory fixtures.
// It never proxies to a real server, device, messaging or call service.
const initial = structuredClone({ contacts, threads, calls, recordings })
const operations = []
async function readJSON(request) {
  let body = ''
  for await (const chunk of request) {
    body += chunk
    if (body.length > 65536) throw new Error('Fixture request too large')
  }
  return body ? JSON.parse(body) : {}
}

function applyState(items, predicate, action) {
  for (let i = items.length - 1; i >= 0; i--) {
    const item = items[i]
    if (!predicate(item)) continue
    if (action === 'delete') items.splice(i, 1)
    if (action === 'favorite' || action === 'unfavorite') item.favorite = action === 'favorite'
    if (action === 'read' || action === 'unread') {
      if ('unread_count' in item) {
        item.unread_count = action === 'read' ? 0 : item.unread_count
        item.marked_unread = action === 'unread'
      } else if (item.missed) item.read = action === 'read'
    }
  }
}

async function fixtureWrite(request, response, pathname) {
  const body = await readJSON(request)
  if (pathname === '/__uat/reset') {
    for (const [name, items] of Object.entries({ contacts, threads, calls, recordings })) {
      items.splice(0, items.length, ...structuredClone(initial[name]))
    }
    online = true
    operations.length = 0
  } else if (pathname === '/__uat/connectivity') {
    online = body.online !== false
  } else if (pathname === '/api/v1/messages/read') {
    applyState(threads, item => item.line_id === body.line_id && item.peer === body.peer, 'read')
  } else if (pathname === '/api/v1/messages/threads/state') {
    applyState(threads, item => body.threads?.some(target => target.line_id === item.line_id && target.peer === item.peer), body.action)
  } else if (pathname === '/api/v1/calls/batch') {
    applyState(calls, item => body.ids?.includes(item.id), body.action)
    if (body.action === 'delete') applyState(recordings, item => body.ids?.includes(item.call.id), 'delete')
  } else if (pathname === '/api/v1/recordings/batch') {
    applyState(recordings, item => body.recordings?.some(target => target.id === item.segment.id && target.call_id === item.call.id), body.action)
  } else {
    send(response, 409, { code: 'uat_write_disabled', message: 'This fixture operation is not enabled' })
    return
  }
  if (pathname.startsWith('/api/')) operations.push({ pathname, action: body.action ?? 'read' })
  send(response, 200, { ok: true })
}

const activeStreams = new Set()
const server = https.createServer(
  {
    cert: await readFile(certificatePath),
    key: await readFile(keyPath)
  },
  async (request, response) => {
    const url = new URL(request.url || '/', `https://${request.headers.host || 'localhost'}`)
    let status = 200
    if (!authorize(request, response)) {
      status = 401
    } else if (mutable && url.pathname === '/__uat/state' && request.method === 'GET') {
      send(response, 200, { online, contacts, threads, calls, recordings, operations })
    } else if (mutable && url.pathname.startsWith('/__uat/') && request.method === 'POST') {
      try { await fixtureWrite(request, response, url.pathname) } catch { send(response, 400, { code: 'invalid_fixture_request' }) }
    } else if (!online) {
      status = 503
      send(response, status, { code: 'uat_offline', message: 'The local fixture server is temporarily unavailable' })
    } else if (
      request.method === 'GET' &&
      (url.pathname === '/api/v1/runtime/events' || url.pathname === '/api/v1/messages/events')
    ) {
      response.writeHead(200, {
        'Cache-Control': 'no-store',
        'Content-Type': 'text/event-stream; charset=utf-8',
        Connection: 'keep-alive'
      })
      response.write(': ios-uat-connected\n\n')
      const timer = setInterval(() => response.write(': keepalive\n\n'), 10_000)
      activeStreams.add(timer)
      request.on('close', () => {
        clearInterval(timer)
        activeStreams.delete(timer)
      })
    } else if (request.method === 'GET') {
      const body = readOnlyBody(url.pathname)
      if (body !== undefined) {
        send(response, 200, body)
      } else if (url.pathname === '/api/v1/network' || url.pathname === '/api/v1/proxies') {
        status = 403
        send(response, status, {
          code: 'forbidden',
          message: 'This read-only UAT user cannot access network administration'
        })
      } else {
        status = 404
        send(response, status, { code: 'not_found', message: 'Not found' })
      }
    } else if (mutable && request.method === 'PATCH') {
      try { await fixtureWrite(request, response, url.pathname) } catch { send(response, 400, { code: 'invalid_fixture_request' }) }
    } else {
      status = 409
      send(response, status, {
        code: 'uat_write_disabled',
        message: 'This local iOS UAT server is read-only'
      })
    }
    process.stdout.write(`${request.method || 'GET'} ${url.pathname} ${status}\n`)
  }
)

server.listen(port, '127.0.0.1', () => {
  process.stdout.write(`ModemDeck iOS ${mutable ? 'in-memory mutable' : 'read-only'} UAT server listening on https://127.0.0.1:${port}\n`)
})

function shutdown() {
  for (const timer of activeStreams) clearInterval(timer)
  server.close(() => process.exit(0))
}

process.on('SIGINT', shutdown)
process.on('SIGTERM', shutdown)
