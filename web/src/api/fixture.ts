import type { ListQuery, MessageQuery, ModemDeckGateway } from './gateway'
import type {
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
  DeviceFeatureCapability,
  DeviceHardwareConfiguration,
  GlobalCallSettings,
  IncomingCallPolicy,
  LineSummary,
  Message,
  MessageThread,
  RecordingSettings,
  SendMessageInput,
  TelegramUnit,
  TelegramUnitInput,
  UpdateDeviceConfigurationInput,
  UpdateGlobalCallSettingsInput
} from './types'
import { ApiError } from './types'
import { threadKey } from './normalize'

const MAIN_ICCID = '8986012345678900001'
const TRAVEL_ICCID = '8984045678901230002'

const contacts: Contact[] = [
  {
    id: 'contact-alex',
    display_name: 'Alex Rowan',
    phones: [{ id: 'phone-alex', label: '手机', number: '+1 202 555 0103', primary: true }],
    notes: '东京',
    revision: 3
  },
  {
    id: 'contact-casey',
    display_name: 'Casey Morgan',
    phones: [
      { id: 'phone-casey-mobile', label: '手机', number: '+1 202 555 0104', primary: true },
      { id: 'phone-casey-work', label: '工作', number: '+1 202 555 0105', primary: false }
    ],
    revision: 2
  },
  {
    id: 'contact-riley',
    display_name: 'Riley Quinn',
    phones: [{ id: 'phone-riley', label: '手机', number: '+1 202 555 0106', primary: true }],
    revision: 1
  }
]

const threads: MessageThread[] = [
  {
    key: threadKey(MAIN_ICCID, '+1 202 555 0103'),
    imsi: '001010000000001',
    iccid: MAIN_ICCID,
    peer: '+1 202 555 0103',
    contact_name: 'Alex Rowan',
    last_timestamp: '2026-07-23T09:42:00Z',
    last_content: '好的，明天下午联系。',
    unread_count: 1
  },
  {
    key: threadKey(TRAVEL_ICCID, '+1 202 555 0104'),
    imsi: '001020000000002',
    iccid: TRAVEL_ICCID,
    peer: '+1 202 555 0104',
    contact_name: 'Casey Morgan',
    last_timestamp: '2026-07-22T14:18:00Z',
    last_content: 'The demo workspace is ready.',
    unread_count: 0
  },
  {
    key: threadKey(MAIN_ICCID, '+1 202 555 0106'),
    imsi: '001010000000001',
    iccid: MAIN_ICCID,
    peer: '+1 202 555 0106',
    contact_name: 'Riley Quinn',
    last_timestamp: '2026-07-20T06:05:00Z',
    last_content: '收到，谢谢。',
    unread_count: 0
  }
]

const messagesByThread: Record<string, Message[]> = {
  [threadKey(MAIN_ICCID, '+1 202 555 0103')]: [
    {
      id: '101',
      imsi: '001010000000001',
      iccid: MAIN_ICCID,
      peer: '+1 202 555 0103',
      direction: 'outgoing',
      content: '设备已经恢复，可以再试一次。',
      timestamp: '2026-07-23T09:38:00Z',
      type: 2,
      status: 2
    },
    {
      id: '102',
      imsi: '001010000000001',
      iccid: MAIN_ICCID,
      peer: '+1 202 555 0103',
      direction: 'incoming',
      content: '好的，明天下午联系。',
      timestamp: '2026-07-23T09:42:00Z',
      type: 1,
      status: 1
    }
  ],
  [threadKey(TRAVEL_ICCID, '+1 202 555 0104')]: [
    {
      id: '103',
      imsi: '001020000000002',
      iccid: TRAVEL_ICCID,
      peer: '+1 202 555 0104',
      direction: 'incoming',
      content: 'The demo workspace is ready.',
      timestamp: '2026-07-22T14:18:00Z',
      type: 1,
      status: 1
    }
  ],
  [threadKey(MAIN_ICCID, '+1 202 555 0106')]: [
    {
      id: '104',
      imsi: '001010000000001',
      iccid: MAIN_ICCID,
      peer: '+1 202 555 0106',
      direction: 'outgoing',
      content: '配置已发送。',
      timestamp: '2026-07-20T05:59:00Z',
      type: 2,
      status: 2
    },
    {
      id: '105',
      imsi: '001010000000001',
      iccid: MAIN_ICCID,
      peer: '+1 202 555 0106',
      direction: 'incoming',
      content: '收到，谢谢。',
      timestamp: '2026-07-20T06:05:00Z',
      type: 1,
      status: 1
    }
  ]
}

const calls: CallRecord[] = [
  {
    id: 'call-1',
    device_id: 'fixture-001',
    direction: 'incoming',
    remote_number: '+1 202 555 0103',
    display_name: 'Alex Rowan',
    contact_id: 'contact-alex',
    started_at: '2026-07-23T08:52:00Z',
    ended_at: '2026-07-23T08:57:12Z',
    duration_seconds: 312,
    missed: false
  },
  {
    id: 'call-2',
    device_id: 'fixture-001',
    direction: 'incoming',
    remote_number: '+1 202 555 0107',
    started_at: '2026-07-22T11:14:00Z',
    ended_at: '2026-07-22T11:14:31Z',
    duration_seconds: 0,
    missed: true
  },
  {
    id: 'call-3',
    device_id: 'fixture-002',
    direction: 'outgoing',
    remote_number: '+1 202 555 0104',
    display_name: 'Casey Morgan',
    contact_id: 'contact-casey',
    started_at: '2026-07-22T07:30:00Z',
    ended_at: '2026-07-22T07:33:46Z',
    duration_seconds: 226,
    missed: false
  }
]

function clone<T>(value: T): T {
  return structuredClone(value)
}

function includes(value: string | undefined, query: string): boolean {
  return (value || '').toLocaleLowerCase().includes(query)
}

function normalizedQuery(query: ListQuery = {}): string {
  return (query.q || '').trim().toLocaleLowerCase()
}

let fixtureRecordingURL = ''

function recordingFixtureURL(): string {
  if (fixtureRecordingURL) return fixtureRecordingURL
  const sampleRate = 8000
  const sampleCount = sampleRate
  const bytes = new Uint8Array(44 + sampleCount * 2)
  const view = new DataView(bytes.buffer)
  const write = (offset: number, value: string) => {
    for (let index = 0; index < value.length; index += 1) {
      view.setUint8(offset + index, value.charCodeAt(index))
    }
  }
  write(0, 'RIFF')
  view.setUint32(4, bytes.length - 8, true)
  write(8, 'WAVE')
  write(12, 'fmt ')
  view.setUint32(16, 16, true)
  view.setUint16(20, 1, true)
  view.setUint16(22, 1, true)
  view.setUint32(24, sampleRate, true)
  view.setUint32(28, sampleRate * 2, true)
  view.setUint16(32, 2, true)
  view.setUint16(34, 16, true)
  write(36, 'data')
  view.setUint32(40, sampleCount * 2, true)
  for (let index = 0; index < sampleCount; index += 1) {
    const envelope = Math.min(1, index / 200, (sampleCount - index) / 200)
    const sample = Math.sin((2 * Math.PI * 440 * index) / sampleRate) * envelope
    view.setInt16(44 + index * 2, Math.round(sample * 5000), true)
  }
  fixtureRecordingURL = URL.createObjectURL(new Blob([bytes], { type: 'audio/wav' }))
  return fixtureRecordingURL
}

export type FixtureGatewayOptions = {
  lineCount?: number
  noDevices?: boolean
}

function fixtureLines(count: number): LineSummary[] {
  const result: LineSummary[] = []
  if (count >= 1) {
    result.push({
      id: 'line-fixture-main',
      iccid: MAIN_ICCID,
      imsi: '001010000000001',
      phone_number: '+1 202 555 0101',
      operator: 'Aurora Mobile',
      device_imei: 'fixture-001',
      device_alias: 'Main cellular line',
      state: 'registered',
      capabilities: {
        dial: true,
        answer: true,
        reject: true,
        hangup: true,
        dtmf: true,
        message: true
      }
    })
  }
  if (count >= 2) {
    result.push({
      id: 'line-fixture-travel',
      iccid: TRAVEL_ICCID,
      imsi: '001020000000002',
      phone_number: '+1 202 555 0102',
      operator: 'Pine Wireless',
      device_imei: 'fixture-002',
      device_alias: 'Travel cellular line',
      state: 'registered',
      capabilities: {
        dial: true,
        answer: true,
        reject: false,
        hangup: true,
        dtmf: false,
        message: true
      }
    })
  }
  for (let index = result.length; index < count; index += 1) {
    const displayIndex = index + 1
    result.push({
      id: `line-fixture-${displayIndex}`,
      iccid: `89860000000000000${String(displayIndex).padStart(2, '0')}`,
      imsi: `0010100000000${String(displayIndex).padStart(2, '0')}`,
      phone_number: `+1 202 555 ${String(displayIndex).padStart(4, '0')}`,
      operator: `Fixture Network ${displayIndex}`,
      device_imei: `fixture-${String(displayIndex).padStart(3, '0')}`,
      device_alias: `Cellular line ${displayIndex}`,
      state: 'registered',
      capabilities: {
        dial: true,
        answer: true,
        reject: displayIndex % 2 === 1,
        hangup: true,
        dtmf: true,
        message: true
      }
    })
  }
  return result
}

function feature(
  backend: string,
  overrides: Partial<DeviceFeatureCapability> = {}
): DeviceFeatureCapability {
  return {
    backend,
    supported: true,
    implemented: true,
    readable: true,
    writable: true,
    ...overrides
  }
}

function fixtureHardware(line: LineSummary, index: number): DeviceHardwareConfiguration {
  const volteAvailable = index === 0
  return {
    line_id: line.id || '',
    revision: 'fixture-revision-1',
    observed_at: '2026-07-23T12:00:00Z',
    identity: {
      manufacturer: 'Fixture',
      model: `Modem ${index + 1}`,
      firmware: 'fixture-fw-1',
      equipment_identifier: line.device_imei
    },
    radio: {
      enabled: true,
      enabled_known: true,
      power_state: 'on',
      power_state_code: 3
    },
    flight_mode: false,
    flight_mode_known: true,
    network_enabled: index === 0,
    data_connections:
      index === 0
        ? [
            {
              id: 'bearer-fixture-main',
              connected: true,
              apn: 'fixture',
              ip_family: 'ipv4',
              interface: 'wwan0',
              ipv4: {
                method: 'static',
                address: '10.0.0.2',
                prefix: 30,
                gateway: '10.0.0.1',
                dns: ['1.1.1.1'],
                mtu: 1500
              },
              ipv6: {
                method: '',
                address: '',
                prefix: 0,
                gateway: '',
                dns: [],
                mtu: 0
              }
            }
          ]
        : [],
    volte: {
      policy_known: volteAvailable,
      ...(volteAvailable ? { policy: 'enabled' as const, profile_id: 'fixture-volte-v1' } : {})
    },
    capabilities: {
      radio: feature('modemmanager'),
      data_connection: feature('modemmanager'),
      flight_mode: feature('modemmanager', {
        reason: 'represented by radio.enabled; disabling the modem enters low-power state'
      }),
      vowifi: feature('vendor_extension', {
        supported: false,
        implemented: false,
        readable: false,
        writable: false,
        reason: 'ModemManager has no generic VoWiFi policy interface'
      }),
      volte: feature('vendor_extension', {
        supported: volteAvailable,
        implemented: volteAvailable,
        readable: volteAvailable,
        writable: volteAvailable,
        reason: volteAvailable
          ? undefined
          : 'no exact manufacturer, model, and firmware profile was resolved'
      }),
      alias: feature('application', {
        supported: false,
        implemented: false,
        readable: false,
        writable: false,
        reason: 'display aliases are owned by the ModemDeck application database'
      }),
      esim: feature('vendor_extension', {
        supported: false,
        implemented: false,
        readable: false,
        writable: false,
        reason: 'eSIM profile provisioning is unavailable'
      }),
      at_terminal: feature('vendor_extension', {
        supported: false,
        implemented: false,
        readable: false,
        writable: false,
        reason: 'arbitrary AT access is intentionally not exposed'
      }),
      ussd: feature('modemmanager', {
        implemented: false,
        readable: false,
        writable: false,
        reason: 'USSD is an operational session API'
      }),
      connection_profile: feature('modemmanager', {
        implemented: false,
        readable: false,
        writable: false,
        reason: 'connection profile mutation is deferred'
      })
    }
  }
}

export function createFixtureGateway(options: FixtureGatewayOptions = {}): ModemDeckGateway {
  const requestedLineCount = Math.max(0, Math.trunc(options.lineCount ?? 2))
  const lines = options.noDevices ? [] : fixtureLines(requestedLineCount)
  const fixtureDeviceList = options.noDevices
    ? []
    : lines.map((line, index): Device => ({
        imei: line.device_imei,
        alias: line.device_alias,
        model: `Fixture modem ${index + 1}`,
        firmware: 'Fixture 1.0',
        state: line.state,
        current_iccid: line.iccid,
        sim_inserted: true,
        signal_dbm: -70 - index,
        capabilities: clone(line.capabilities || {}),
        sim: {
          iccid: line.iccid,
          imsi: line.imsi,
          phone_number: line.phone_number,
          operator: line.operator,
          current_imei: line.device_imei,
          reg_status: 1,
          reg_status_text: 'registered',
          lac: '',
          cell_id: '',
          apn: index === 0 ? 'fixture' : '',
          ims_status: index === 0 ? 1 : 0,
          last_seen: '2026-07-23T12:00:00Z'
        }
      }))
  const hardwareByLine = new Map(
    lines.map((line, index) => [line.id || '', fixtureHardware(line, index)])
  )
  const hardwareRevisionByLine = new Map(lines.map(line => [line.id || '', 1]))
  const policyByLine = new Map(
    lines.map(line => [
      line.id || '',
      {
        policy: 'follow_global' as IncomingCallPolicy,
        revision: 1,
        updated_at: '2026-07-23 12:00:00'
      }
    ])
  )
  let sequence = 200
  let globalCallSettings: GlobalCallSettings = {
    receive_calls: true,
    revision: 1
  }
  let activeCall: CallSession | undefined
  let activeCallRecording: CallRecordingState | undefined
  let callPolls = 0
  let recordingSettings: RecordingSettings = {
    default_enabled: false,
    revision: 1
  }
  const telegramUnits: TelegramUnit[] = [
    {
      id: 'telegram-main',
      display_name: '主线路通知',
      enabled: true,
      chat_id: '-1001234567890',
      admin_id: '100000001',
      line_scopes: ['line-fixture-main'],
      incoming_sms: true,
      missed_calls: true,
      token_configured: true,
      bot_username: 'modemdeck_main_bot',
      revision: 3
    },
    {
      id: 'telegram-travel',
      display_name: '旅行线路通知',
      enabled: false,
      chat_id: '-1001234567891',
      admin_id: '100000002',
      line_scopes: ['line-fixture-travel'],
      incoming_sms: true,
      missed_calls: false,
      token_configured: true,
      bot_username: 'modemdeck_travel_bot',
      revision: 2
    }
  ]

  function configurationForLine(lineID: string): DeviceConfiguration {
    const line = lines.find(candidate => candidate.id === lineID)
    const hardware = hardwareByLine.get(lineID)
    const policy = policyByLine.get(lineID)
    if (!line || !hardware || !policy) {
      throw new ApiError('线路不存在', 404, 'not_found')
    }
    const rejectAvailable = line.capabilities?.reject === true
    const effectivePolicy =
      policy.policy === 'follow_global'
        ? globalCallSettings.receive_calls
          ? 'receive'
          : 'do_not_disturb'
        : policy.policy
    return {
      hardware: clone(hardware),
      incoming_calls: {
        policy: policy.policy,
        revision: policy.revision,
        effective_policy: effectivePolicy,
        global_receive_calls: globalCallSettings.receive_calls,
        global_revision: globalCallSettings.revision,
        updated_at: policy.updated_at,
        enforcement: {
          mode: 'one_shot_reject',
          max_submissions_per_call: 1,
          new_incoming_ringing_only: true,
          available: rejectAvailable,
          config_only: !rejectAvailable,
          ...(!rejectAvailable
            ? { reason: 'the attached line does not currently advertise reject-call capability' }
            : {})
        }
      }
    }
  }

  function advanceHardwareRevision(lineID: string, hardware: DeviceHardwareConfiguration): void {
    const revision = (hardwareRevisionByLine.get(lineID) || 1) + 1
    hardwareRevisionByLine.set(lineID, revision)
    hardware.revision = `fixture-revision-${revision}`
    hardware.observed_at = '2026-07-23T12:01:00Z'
  }

  return {
    async getBootstrap(): Promise<BootstrapResponse> {
      return {
        capabilities: {
          agent_connected: true,
          dial: true,
          message: true,
          webrtc_audio: true,
          device_control: true,
          volte_control: true,
          vowifi_control: true,
          unavailable_reasons: {}
        },
        lines: clone(lines)
      }
    },

    async listContacts(query: ListQuery = {}): Promise<Contact[]> {
      const q = normalizedQuery(query)
      return clone(
        contacts.filter(
          contact =>
            !q ||
            includes(contact.display_name, q) ||
            contact.phones.some(phone => includes(phone.number, q))
        )
      )
    },

    async createContact(input: ContactInput): Promise<Contact> {
      sequence += 1
      const contact: Contact = {
        id: `contact-fixture-${sequence}`,
        display_name: input.display_name,
        notes: input.notes,
        revision: 1,
        phones: input.phones.map((phone, index) => ({
          id: `phone-fixture-${sequence}-${index + 1}`,
          label: phone.label,
          number: phone.number,
          primary: phone.primary
        }))
      }
      contacts.push(contact)
      return clone(contact)
    },

    async updateContact(id: string, input: ContactInput): Promise<Contact> {
      const index = contacts.findIndex(contact => contact.id === id)
      const current = contacts[index]
      if (index < 0 || !current) throw new ApiError('联系人不存在', 404)
      const updated: Contact = {
        ...current,
        display_name: input.display_name,
        notes: input.notes,
        revision: (current.revision || 0) + 1,
        phones: input.phones.map((phone, phoneIndex) => ({
          id: phone.id || `phone-fixture-${sequence}-${phoneIndex + 1}`,
          label: phone.label,
          number: phone.number,
          primary: phone.primary
        }))
      }
      contacts[index] = updated
      return clone(updated)
    },

    async deleteContact(id: string): Promise<void> {
      const index = contacts.findIndex(contact => contact.id === id)
      if (index < 0) throw new ApiError('联系人不存在', 404)
      contacts.splice(index, 1)
    },

    async listThreads(query: ListQuery = {}): Promise<MessageThread[]> {
      const q = normalizedQuery(query)
      return clone(
        threads
          .filter(
            thread =>
              !q ||
              includes(thread.contact_name, q) ||
              includes(thread.peer, q) ||
              includes(thread.last_content, q)
          )
          .sort((a, b) => Date.parse(b.last_timestamp) - Date.parse(a.last_timestamp))
      )
    },

    async listMessages(query: MessageQuery): Promise<Message[]> {
      return clone(messagesByThread[threadKey(query.iccid, query.peer)] || [])
    },

    async sendMessage(input: SendMessageInput): Promise<Message> {
      sequence += 1
      const iccid =
        input.iccid ||
        (input.line_id === 'line-fixture-main'
          ? MAIN_ICCID
          : input.line_id === 'line-fixture-travel'
            ? TRAVEL_ICCID
            : '')
      if (!iccid) throw new ApiError('请选择线路', 400)
      const key = input.thread_key || threadKey(iccid, input.to)
      let thread = threads.find(item => item.key === key)
      if (!thread) {
        const contact = contacts.find(item => item.phones.some(phone => phone.number === input.to))
        thread = {
          key,
          imsi: '',
          iccid,
          peer: input.to,
          contact_name: contact?.display_name,
          last_timestamp: '2026-07-23T12:00:00Z',
          unread_count: 0
        }
        threads.unshift(thread)
        messagesByThread[key] = []
      }
      const message: Message = {
        id: String(sequence),
        imsi: thread.imsi,
        iccid,
        peer: input.to,
        direction: 'outgoing',
        content: input.content,
        timestamp: '2026-07-23T12:00:00Z',
        type: 2,
        status: 2
      }
      messagesByThread[key]?.push(message)
      thread.last_content = message.content
      thread.last_timestamp = message.timestamp
      return clone(message)
    },

    async listCalls(filter: CallFilter = 'all', query: ListQuery = {}): Promise<CallRecord[]> {
      const q = normalizedQuery(query)
      return clone(
        calls.filter(call => {
          const filterMatch =
            filter === 'all' ||
            (filter === 'missed' && call.missed) ||
            (filter === 'incoming' && call.direction === 'incoming') ||
            (filter === 'outgoing' && call.direction === 'outgoing')
          return filterMatch && (!q || includes(call.display_name, q) || includes(call.remote_number, q))
        })
      )
    },

    async getActiveCalls(): Promise<CallSession[]> {
      if (!activeCall || activeCall.phase === 'ended' || activeCall.phase === 'failed') return []
      callPolls += 1
      if (activeCall.phase === 'dialing' && callPolls >= 1) activeCall.phase = 'ringing'
      else if (activeCall.phase === 'ringing' && callPolls >= 2) {
        activeCall.phase = 'active'
        activeCall.active_at = '2026-07-23T12:05:04Z'
        activeCall.bearer = 'volte'
      }
      return [clone(activeCall)]
    },

    async startCall(
      lineKey: string,
      number: string,
      recordingEnabled?: boolean
    ): Promise<CallSession> {
      sequence += 1
      callPolls = 0
      const contact = contacts.find(item => item.phones.some(phone => phone.number === number))
      activeCall = {
        id: `call-fixture-${sequence}`,
        line_key: lineKey,
        direction: 'outgoing',
        remote_number: number,
        display_name: contact?.display_name,
        phase: 'dialing',
        media_available: false,
        created_at: '2026-07-23T12:05:00Z'
      }
      activeCallRecording = {
        call_id: activeCall.id,
        enabled: recordingEnabled ?? recordingSettings.default_enabled,
        active: false
      }
      return clone(activeCall)
    },

    async callAction(id: string, action: 'answer' | 'reject' | 'hangup'): Promise<CallSession> {
      if (!activeCall || activeCall.id !== id) throw new ApiError('通话不存在', 404)
      if (action === 'answer') {
        activeCall.phase = 'active'
        activeCall.active_at = '2026-07-23T12:05:04Z'
        activeCall.bearer = 'volte'
        if (activeCallRecording?.enabled) {
          activeCallRecording.active = true
          activeCallRecording.started_at = activeCall.active_at
        }
      } else {
        activeCall.phase = 'ended'
        activeCall.ended_at = '2026-07-23T12:08:00Z'
        if (activeCallRecording) activeCallRecording.active = false
      }
      return clone(activeCall)
    },

    async sendDTMF(id: string, digit: string): Promise<CallSession> {
      if (!activeCall || activeCall.id !== id) throw new ApiError('通话不存在', 404)
      if (activeCall.phase !== 'active') throw new ApiError('通话尚未接通', 409)
      if (!/^[0-9*#A-D]$/.test(digit)) throw new ApiError('DTMF 按键无效', 400)
      return clone(activeCall)
    },

    async exchangeCallMedia(): Promise<string> {
      throw new ApiError('测试数据未连接音频设备', 503)
    },

    async getRecordingSettings(): Promise<RecordingSettings> {
      return clone(recordingSettings)
    },

    async updateRecordingSettings(settings: RecordingSettings): Promise<RecordingSettings> {
      if (settings.revision !== recordingSettings.revision) {
        throw new ApiError('录音设置已被修改', 409, 'conflict')
      }
      recordingSettings = {
        default_enabled: settings.default_enabled,
        revision: recordingSettings.revision + 1
      }
      return clone(recordingSettings)
    },

    async setCallRecording(id: string, enabled: boolean): Promise<CallRecordingState> {
      if (!activeCall || activeCall.id !== id || !activeCallRecording) {
        throw new ApiError('通话不存在', 404)
      }
      activeCallRecording.enabled = enabled
      activeCallRecording.active = enabled && activeCall.phase === 'active'
      activeCallRecording.error = undefined
      activeCallRecording.started_at = activeCallRecording.active
        ? activeCall.active_at || '2026-07-23T12:05:04Z'
        : undefined
      return clone(activeCallRecording)
    },

    async listCallRecordings(id: string): Promise<CallRecording[]> {
      if (id !== 'call-1' && id !== 'call-3') return []
      return [
        {
          id: `recording-${id}`,
          call_id: id,
          started_at: id === 'call-1' ? '2026-07-23T08:52:04Z' : '2026-07-22T07:30:03Z',
          ended_at: id === 'call-1' ? '2026-07-23T08:52:05Z' : '2026-07-22T07:30:04Z',
          duration_seconds: 1,
          content_type: 'audio/wav',
          size_bytes: 16044,
          download_url: recordingFixtureURL()
        }
      ]
    },

    async listDevices(): Promise<Device[]> {
      return clone(fixtureDeviceList)
    },

    async getGlobalCallSettings(): Promise<GlobalCallSettings> {
      return clone(globalCallSettings)
    },

    async updateGlobalCallSettings(
      input: UpdateGlobalCallSettingsInput
    ): Promise<GlobalCallSettings> {
      if (input.expected_revision !== globalCallSettings.revision) {
        throw new ApiError('来电设置已被其他会话修改', 409, 'conflict')
      }
      globalCallSettings = {
        ...globalCallSettings,
        receive_calls: input.receive_calls,
        revision: globalCallSettings.revision + 1
      }
      return clone(globalCallSettings)
    },

    async getDeviceConfiguration(lineID: string): Promise<DeviceConfiguration> {
      return configurationForLine(lineID)
    },

    async updateDeviceConfiguration(
      lineID: string,
      input: UpdateDeviceConfigurationInput
    ): Promise<DeviceConfiguration> {
      const current = configurationForLine(lineID)
      if (input.operation === 'set_incoming_call_policy') {
        const policy = policyByLine.get(lineID)
        if (!policy) throw new ApiError('线路不存在', 404, 'not_found')
        if (input.expected_policy_revision !== policy.revision) {
          throw new ApiError('线路来电策略已被其他会话修改', 409, 'conflict')
        }
        policy.policy = input.incoming_call_policy
        policy.revision += 1
        policy.updated_at = '2026-07-23 12:01:00'
        return { incoming_calls: configurationForLine(lineID).incoming_calls }
      }

      const hardware = hardwareByLine.get(lineID)
      if (!hardware) throw new ApiError('线路不存在', 404, 'not_found')
      if (input.expected_device_revision !== current.hardware?.revision) {
        throw new ApiError('设备配置已被其他会话修改', 409, 'conflict')
      }
      if (!input.request_id.trim()) {
        throw new ApiError('request_id 不能为空', 400, 'invalid_argument')
      }
      switch (input.operation) {
        case 'set_radio_enabled':
          hardware.radio.enabled = input.radio_enabled
          hardware.radio.enabled_known = true
          hardware.radio.power_state = input.radio_enabled ? 'on' : 'low'
          hardware.radio.power_state_code = input.radio_enabled ? 3 : 2
          hardware.flight_mode = !input.radio_enabled
          hardware.flight_mode_known = true
          break
        case 'connect_data':
          hardware.network_enabled = true
          hardware.data_connections = [
            {
              id: `bearer-${lineID}`,
              connected: true,
              apn: input.apn,
              ip_family: input.ip_family,
              interface: `wwan-${lineID}`,
              ipv4: {
                method: input.ip_family === 'ipv6' ? '' : 'static',
                address: input.ip_family === 'ipv6' ? '' : '10.0.0.2',
                prefix: input.ip_family === 'ipv6' ? 0 : 30,
                gateway: input.ip_family === 'ipv6' ? '' : '10.0.0.1',
                dns: input.ip_family === 'ipv6' ? [] : ['1.1.1.1'],
                mtu: input.ip_family === 'ipv6' ? 0 : 1500
              },
              ipv6: {
                method: input.ip_family === 'ipv4' ? '' : 'static',
                address: input.ip_family === 'ipv4' ? '' : '2001:db8::2',
                prefix: input.ip_family === 'ipv4' ? 0 : 64,
                gateway: input.ip_family === 'ipv4' ? '' : '2001:db8::1',
                dns: input.ip_family === 'ipv4' ? [] : ['2606:4700:4700::1111'],
                mtu: input.ip_family === 'ipv4' ? 0 : 1500
              }
            }
          ]
          break
        case 'disconnect_data':
          hardware.network_enabled = false
          hardware.data_connections = []
          break
        case 'set_volte_policy':
          if (!hardware.capabilities.volte.writable) {
            throw new ApiError(
              hardware.capabilities.volte.reason || 'VoLTE 配置不可写',
              501,
              'not_supported'
            )
          }
          hardware.volte = {
            ...hardware.volte,
            policy_known: true,
            policy: input.volte_policy
          }
          break
      }
      advanceHardwareRevision(lineID, hardware)
      return { hardware: clone(hardware) }
    },

    async listTelegramUnits(): Promise<TelegramUnit[]> {
      return clone(telegramUnits)
    },

    async createTelegramUnit(input: TelegramUnitInput): Promise<TelegramUnit> {
      if (!input.bot_token) throw new ApiError('新建 Bot 需要 token', 400)
      sequence += 1
      const unit: TelegramUnit = {
        id: `telegram-fixture-${sequence}`,
        display_name: input.display_name,
        enabled: input.enabled,
        chat_id: input.chat_id,
        admin_id: input.admin_id,
        line_scopes: [...input.line_scopes],
        incoming_sms: input.incoming_sms,
        missed_calls: input.missed_calls,
        token_configured: true,
        revision: 1
      }
      telegramUnits.push(unit)
      return clone(unit)
    },

    async updateTelegramUnit(id: string, input: TelegramUnitInput): Promise<TelegramUnit> {
      const index = telegramUnits.findIndex(unit => unit.id === id)
      const current = telegramUnits[index]
      if (index < 0 || !current) throw new ApiError('Telegram Bot 不存在', 404)
      if (input.revision !== current.revision) throw new ApiError('Telegram Bot 已被修改', 409)
      const updated: TelegramUnit = {
        ...current,
        display_name: input.display_name,
        enabled: input.enabled,
        chat_id: input.chat_id,
        admin_id: input.admin_id,
        line_scopes: [...input.line_scopes],
        incoming_sms: input.incoming_sms,
        missed_calls: input.missed_calls,
        token_configured: Boolean(input.bot_token) || current.token_configured,
        revision: current.revision + 1
      }
      telegramUnits[index] = updated
      return clone(updated)
    },

    async deleteTelegramUnit(id: string, revision: number): Promise<void> {
      const index = telegramUnits.findIndex(unit => unit.id === id)
      const current = telegramUnits[index]
      if (index < 0 || !current) throw new ApiError('Telegram Bot 不存在', 404)
      if (revision !== current.revision) throw new ApiError('Telegram Bot 已被修改', 409)
      telegramUnits.splice(index, 1)
    }
  }
}
