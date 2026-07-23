import type {
  ConfiguredModemDeckGateway,
  GatewayInteractions,
  ListQuery,
  ModemDeckGateway
} from './gateway'
import {
  callActionContract,
  callMediaContract,
  callRecordingContract,
  communicationContracts,
  createCallActionPayload,
  createCallMediaPayload,
  createCallPayload,
  createCallRecordingPayload,
  createDeviceConfigurationPayload,
  createDTMFPayload,
  createGlobalCallSettingsPayload,
  createMessagePayload,
  createRecordingSettingsPayload,
  createTelegramUnitPayload,
  parseActiveCallsResponse,
  parseCallMediaResponse,
  parseCallRecordingState,
  parseCallRecordingsResponse,
  parseCallResponse,
  parseDeviceConfigurationResponse,
  parseGlobalCallSettings,
  parseMessageResponse,
  parseRecordingSettingsResponse,
  parseTelegramUnitResponse,
  parseTelegramUnitsResponse,
  telegramUnitContract,
  telegramUnitDeletePath,
  deviceConfigurationContract
} from './contract'
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
  CallRecording,
  CallRecordingState,
  CallRecord,
  CallSession,
  Contact,
  ContactInput,
  Device,
  DeviceConfiguration,
  GlobalCallSettings,
  LoginInput,
  Message,
  MessageThread,
  RecordingSettings,
  SendMessageInput,
  SessionResponse,
  TelegramUnit,
  TelegramUnitInput,
  UpdateDeviceConfigurationInput,
  UpdateGlobalCallSettingsInput
} from './types'
import { ApiError } from './types'
import { createFixtureGateway } from './fixture'

const API_ROOT = '/api/v1'

const runtimeEnvironment = import.meta.env

export const fixtureMode =
  Boolean(runtimeEnvironment?.DEV) && runtimeEnvironment?.VITE_MODEMDECK_FIXTURE === '1'

const REAL_INTERACTIONS: GatewayInteractions = {
  contacts: true,
  message: true,
  dial: true,
  telegram: true
}

const FIXTURE_INTERACTIONS: GatewayInteractions = {
  contacts: true,
  message: true,
  dial: true,
  telegram: true
}

let currentCSRFToken = ''
let authenticationRequiredHandler: () => void = () => undefined

function requestID(): string {
  const bytes = globalThis.crypto.getRandomValues(new Uint8Array(16))
  bytes[6] = ((bytes[6] || 0) & 0x0f) | 0x40
  bytes[8] = ((bytes[8] || 0) & 0x3f) | 0x80
  const encoded = Array.from(bytes, byte => byte.toString(16).padStart(2, '0')).join('')
  return `${encoded.slice(0, 8)}-${encoded.slice(8, 12)}-${encoded.slice(12, 16)}-${encoded.slice(16, 20)}-${encoded.slice(20)}`
}

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

function writeJSON(
  path: string,
  method: 'POST' | 'PUT' | 'PATCH',
  input: unknown,
  expectedStatus: number
) {
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

  async sendMessage(input: SendMessageInput): Promise<Message> {
    const contract = communicationContracts.sendMessage
    return parseMessageResponse(
      await writeJSON(
        contract.path,
        contract.method,
        createMessagePayload({ ...input, request_id: input.request_id || requestID() }),
        contract.successStatus
      )
    )
  },

  async listCalls(filter: CallFilter = 'all', query: ListQuery = {}): Promise<CallRecord[]> {
    return parseCalls(
      await get(`${API_ROOT}/calls${queryString({ kind: filter, q: query.q })}`)
    )
  },

  async listDevices(): Promise<Device[]> {
    return parseDevices(await get(`${API_ROOT}/devices`))
  },

  async getGlobalCallSettings(): Promise<GlobalCallSettings> {
    return parseGlobalCallSettings(await get(communicationContracts.getCallSettings.path))
  },

  async updateGlobalCallSettings(
    input: UpdateGlobalCallSettingsInput
  ): Promise<GlobalCallSettings> {
    const contract = communicationContracts.updateCallSettings
    return parseGlobalCallSettings(
      await writeJSON(
        contract.path,
        contract.method,
        createGlobalCallSettingsPayload(input),
        contract.successStatus
      )
    )
  },

  async getDeviceConfiguration(lineID: string): Promise<DeviceConfiguration> {
    const contract = deviceConfigurationContract(lineID).get
    return parseDeviceConfigurationResponse(await get(contract.path))
  },

  async updateDeviceConfiguration(
    lineID: string,
    input: UpdateDeviceConfigurationInput
  ): Promise<DeviceConfiguration> {
    const contract = deviceConfigurationContract(lineID).update
    return parseDeviceConfigurationResponse(
      await writeJSON(
        contract.path,
        contract.method,
        createDeviceConfigurationPayload(input),
        contract.successStatus
      )
    )
  },

  async getActiveCalls(): Promise<CallSession[]> {
    return parseActiveCallsResponse(await get(communicationContracts.activeCalls.path))
  },

  async startCall(
    lineKey: string,
    number: string,
    recordingEnabled?: boolean
  ): Promise<CallSession> {
    const contract = communicationContracts.startCall
    return parseCallResponse(
      await writeJSON(
        contract.path,
        contract.method,
        createCallPayload(lineKey, number, requestID(), recordingEnabled),
        contract.successStatus
      )
    )
  },

  async callAction(id, action): Promise<CallSession> {
    const contract = callActionContract(id, action)
    return parseCallResponse(
      await writeJSON(
        contract.path,
        contract.method,
        createCallActionPayload(action, requestID()),
        contract.successStatus
      )
    )
  },

  async sendDTMF(id: string, digit: string): Promise<CallSession> {
    const contract = callActionContract(id, 'dtmf')
    return parseCallResponse(
      await writeJSON(
        contract.path,
        contract.method,
        createDTMFPayload(digit, requestID()),
        contract.successStatus
      )
    )
  },

  async exchangeCallMedia(id: string, offerSDP: string): Promise<string> {
    const contract = callMediaContract(id)
    return parseCallMediaResponse(
      await writeJSON(
        contract.path,
        contract.method,
        createCallMediaPayload(offerSDP),
        contract.successStatus
      )
    )
  },

  async getRecordingSettings(): Promise<RecordingSettings> {
    const contract = communicationContracts.getRecordingSettings
    return parseRecordingSettingsResponse(await get(contract.path))
  },

  async updateRecordingSettings(settings: RecordingSettings): Promise<RecordingSettings> {
    const contract = communicationContracts.updateRecordingSettings
    return parseRecordingSettingsResponse(
      await writeJSON(
        contract.path,
        contract.method,
        createRecordingSettingsPayload(settings),
        contract.successStatus
      )
    )
  },

  async setCallRecording(id: string, enabled: boolean): Promise<CallRecordingState> {
    const contract = callRecordingContract(id).update
    return parseCallRecordingState(
      await writeJSON(
        contract.path,
        contract.method,
        createCallRecordingPayload(enabled),
        contract.successStatus
      )
    )
  },

  async listCallRecordings(id: string): Promise<CallRecording[]> {
    const contract = callRecordingContract(id).list
    return parseCallRecordingsResponse(await get(contract.path))
  },

  async listTelegramUnits(): Promise<TelegramUnit[]> {
    return parseTelegramUnitsResponse(await get(communicationContracts.listTelegram.path))
  },

  async createTelegramUnit(input: TelegramUnitInput): Promise<TelegramUnit> {
    const contract = communicationContracts.createTelegram
    return parseTelegramUnitResponse(
      await writeJSON(
        contract.path,
        contract.method,
        createTelegramUnitPayload(input),
        contract.successStatus
      )
    )
  },

  async updateTelegramUnit(id: string, input: TelegramUnitInput): Promise<TelegramUnit> {
    const contract = telegramUnitContract(id).update
    return parseTelegramUnitResponse(
      await writeJSON(
        contract.path,
        contract.method,
        createTelegramUnitPayload(input),
        contract.successStatus
      )
    )
  },

  async deleteTelegramUnit(id: string, revision: number): Promise<void> {
    const contract = telegramUnitContract(id).delete
    await request(
      telegramUnitDeletePath(id, revision),
      {
        method: contract.method,
        headers: { Accept: 'application/json' }
      },
      contract.successStatus
    )
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
