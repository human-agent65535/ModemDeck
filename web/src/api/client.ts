import type { ListQuery, ModemDeckGateway } from './gateway'
import {
  parseBootstrap,
  parseCalls,
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
  Device,
  Message,
  MessageThread
} from './types'
import { ApiError } from './types'
import { createFixtureGateway } from './fixture'

const API_ROOT = '/api/v1'

export const fixtureMode = import.meta.env.DEV && import.meta.env.VITE_MODEMDECK_FIXTURE === '1'

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

async function get(path: string): Promise<unknown> {
  let response: Response
  try {
    response = await fetch(path, {
      method: 'GET',
      headers: { Accept: 'application/json' },
      credentials: 'same-origin'
    })
  } catch {
    throw new ApiError('无法连接 ModemDeck 服务')
  }

  const contentType = response.headers.get('content-type') || ''
  const body: unknown = contentType.includes('application/json')
    ? await response.json()
    : await response.text()
  if (!response.ok) {
    const details = recordValue(body) as ApiErrorBody | undefined
    const message =
      details?.message ||
      details?.detail ||
      (typeof body === 'string' && body.trim() ? body.trim() : `请求失败（${response.status}）`)
    throw new ApiError(message, response.status, details?.code)
  }
  return body
}

const realGateway: ModemDeckGateway = {
  interactive: false,

  async getBootstrap(): Promise<BootstrapResponse> {
    return parseBootstrap(await get(`${API_ROOT}/bootstrap`))
  },

  async listContacts(query: ListQuery = {}): Promise<Contact[]> {
    return parseContacts(await get(`${API_ROOT}/contacts${queryString({ q: query.q })}`))
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

export const gateway: ModemDeckGateway = fixtureMode ? createFixtureGateway() : realGateway
