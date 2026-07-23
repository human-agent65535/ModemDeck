import type {
  ConfiguredModemDeckGateway,
  GatewayInteractions,
  ListQuery,
  ModemDeckGateway
} from './gateway'
import {
  parseBootstrap,
  parseCalls,
  parseContactResponse,
  parseContacts,
  parseDevices,
  parseMessages,
  parseThreads
} from './normalize'
import type {
  ApiErrorBody,
  BootstrapResponse,
  CallFilter,
  CallRecord,
  Contact,
  ContactInput,
  Device,
  LoginInput,
  Message,
  MessageThread,
  SessionResponse
} from './types'
import { ApiError } from './types'
import { createFixtureGateway } from './fixture'

const API_ROOT = '/api/v1'

export const fixtureMode = import.meta.env.DEV && import.meta.env.VITE_MODEMDECK_FIXTURE === '1'

const REAL_INTERACTIONS: GatewayInteractions = {
  contacts: true,
  message: false,
  dial: false
}

const FIXTURE_INTERACTIONS: GatewayInteractions = {
  contacts: true,
  message: true,
  dial: true
}

let currentCSRFToken = ''
let authenticationRequiredHandler: () => void = () => undefined

export function setClientCSRFToken(token?: string): void {
  currentCSRFToken = token?.trim() || ''
}

export function setAuthenticationRequiredHandler(handler: () => void): void {
  authenticationRequiredHandler = handler
}

function queryString(query: Record<string, string | undefined>): string {
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(query)) {
    if (value) params.set(key, value)
  }
  const encoded = params.toString()
  return encoded ? `?${encoded}` : ''
}

function recordValue(value: unknown): Record<string, unknown> | undefined {
  return value && typeof value === 'object' && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : undefined
}

function stringProperty(source: Record<string, unknown> | undefined, key: string): string | undefined {
  const value = source?.[key]
  return typeof value === 'string' && value.trim() ? value.trim() : undefined
}

function errorDetails(value: unknown): ApiErrorBody | undefined {
  const source = recordValue(value)
  if (!source) return undefined
  return {
    code: stringProperty(source, 'code'),
    message: stringProperty(source, 'message'),
    detail: stringProperty(source, 'detail'),
    field: stringProperty(source, 'field')
  }
}

function parseSession(value: unknown): SessionResponse {
  const source = recordValue(value)
  if (!source || typeof source.authenticated !== 'boolean') {
    throw new ApiError('ModemDeck 服务返回了无效的会话状态', 0, 'invalid_response')
  }

  const session: SessionResponse = {
    authenticated: source.authenticated,
    username: stringProperty(source, 'username'),
    csrf_token: stringProperty(source, 'csrf_token')
  }
  if (session.authenticated && (!session.username || !session.csrf_token)) {
    throw new ApiError('ModemDeck 服务返回了不完整的会话状态', 0, 'invalid_response')
  }
  return session
}

async function responseBody(response: Response): Promise<unknown> {
  if (response.status === 204) return undefined

  const text = await response.text()
  if (!text.trim()) return undefined
  if (!response.headers.get('content-type')?.toLocaleLowerCase().includes('application/json')) {
    return text
  }

  try {
    return JSON.parse(text) as unknown
  } catch {
    if (!response.ok) return text
    throw new ApiError('ModemDeck 服务返回了无效的 JSON', response.status, 'invalid_response')
  }
}

async function request(path: string, init: RequestInit, expectedStatus: number): Promise<unknown> {
  const method = (init.method || 'GET').toUpperCase()
  const headers = new Headers(init.headers)
  if (method !== 'GET' && method !== 'HEAD' && method !== 'OPTIONS' && currentCSRFToken) {
    headers.set('X-ModemDeck-CSRF', currentCSRFToken)
  }

  let response: Response
  try {
    response = await fetch(path, { ...init, headers, credentials: 'same-origin' })
  } catch {
    throw new ApiError('无法连接 ModemDeck 服务')
  }

  let body: unknown
  try {
    body = await responseBody(response)
  } catch (error) {
    if (error instanceof ApiError) throw error
    throw new ApiError('无法读取 ModemDeck 服务响应', response.status, 'invalid_response')
  }

  if (!response.ok) {
    const details = errorDetails(body)
    if (response.status === 401) authenticationRequiredHandler()
    const message =
      details?.message ||
      details?.detail ||
      (typeof body === 'string' && body.trim() ? body.trim() : `请求失败（${response.status}）`)
    throw new ApiError(message, response.status, details?.code, details?.field)
  }
  if (response.status !== expectedStatus) {
    throw new ApiError(
      `ModemDeck 服务返回了意外状态（${response.status}，预期 ${expectedStatus}）`,
      response.status,
      'unexpected_status'
    )
  }
  return body
}

function get(path: string): Promise<unknown> {
  return request(
    path,
    {
      method: 'GET',
      headers: { Accept: 'application/json' }
    },
    200
  )
}

function writeJSON(path: string, method: 'POST' | 'PUT', input: unknown, expectedStatus: number) {
  return request(
    path,
    {
      method,
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json'
      },
      body: JSON.stringify(input)
    },
    expectedStatus
  )
}

const realGateway: ConfiguredModemDeckGateway = {
  interactions: REAL_INTERACTIONS,

  async getSession(): Promise<SessionResponse> {
    return parseSession(await get(`${API_ROOT}/session`))
  },

  async login(input: LoginInput): Promise<SessionResponse> {
    return parseSession(await writeJSON(`${API_ROOT}/session`, 'POST', input, 200))
  },

  async logout(): Promise<void> {
    await request(
      `${API_ROOT}/session`,
      {
        method: 'DELETE',
        headers: { Accept: 'application/json' }
      },
      204
    )
  },

  async getBootstrap(): Promise<BootstrapResponse> {
    return parseBootstrap(await get(`${API_ROOT}/bootstrap`))
  },

  async listContacts(query: ListQuery = {}): Promise<Contact[]> {
    return parseContacts(await get(`${API_ROOT}/contacts${queryString({ q: query.q })}`))
  },

  async createContact(input: ContactInput): Promise<Contact> {
    return parseContactResponse(await writeJSON(`${API_ROOT}/contacts`, 'POST', input, 201))
  },

  async updateContact(id: string, input: ContactInput): Promise<Contact> {
    const contactID = encodeURIComponent(id)
    return parseContactResponse(
      await writeJSON(`${API_ROOT}/contacts/${contactID}`, 'PUT', input, 200)
    )
  },

  async deleteContact(id: string, revision?: number): Promise<void> {
    if (!Number.isSafeInteger(revision) || Number(revision) <= 0) {
      throw new ApiError('删除联系人需要有效的 revision', 0, 'invalid_revision', 'revision')
    }
    const contactID = encodeURIComponent(id)
    await request(
      `${API_ROOT}/contacts/${contactID}${queryString({ revision: String(revision) })}`,
      {
        method: 'DELETE',
        headers: { Accept: 'application/json' }
      },
      204
    )
  },

  async listThreads(query: ListQuery = {}): Promise<MessageThread[]> {
    return parseThreads(await get(`${API_ROOT}/messages/threads${queryString({ q: query.q })}`))
  },

  async listMessages(query): Promise<Message[]> {
    return parseMessages(
      await get(`${API_ROOT}/messages${queryString({ iccid: query.iccid, peer: query.peer })}`)
    )
  },

  async listCalls(filter: CallFilter = 'all', query: ListQuery = {}): Promise<CallRecord[]> {
    return parseCalls(
      await get(`${API_ROOT}/calls${queryString({ kind: filter, q: query.q })}`)
    )
  },

  async listDevices(): Promise<Device[]> {
    return parseDevices(await get(`${API_ROOT}/devices`))
  }
}

function configureFixture(gateway: ModemDeckGateway): ConfiguredModemDeckGateway {
  const session: SessionResponse = {
    authenticated: true,
    username: 'fixture'
  }
  return {
    ...gateway,
    interactions: FIXTURE_INTERACTIONS,
    async getSession() {
      return session
    },
    async login() {
      return session
    },
    async logout() {
      return undefined
    }
  }
}

export const gateway: ConfiguredModemDeckGateway = fixtureMode
  ? configureFixture(createFixtureGateway())
  : realGateway
