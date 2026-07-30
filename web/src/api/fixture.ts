import type { ListQuery, MessageQuery, ModemDeckGateway } from './gateway'
import type {
  ActiveCallSnapshot,
  AboutInfo,
  BootstrapResponse,
  CallFilter,
  CallRecordingSegment,
  CallRecordingState,
  CallRecord,
  CallSession,
  CommandReceipt,
  ConnectionProfile,
  Contact,
  ContactInput,
  CreateMemberInput,
  CreateDeviceInput,
  CreateProxyInput,
  DeleteConnectionProfileInput,
  Device,
  DeviceConfiguration,
  DeviceFeatureCapability,
  DeviceHardwareConfiguration,
  DiagnosticLogEntry,
  DiagnosticLogPage,
  DiagnosticLogQuery,
  DiagnosticLogStreamHandlers,
  DiagnosticsSnapshot,
  GlobalCallSettings,
  IncomingCallPolicy,
  LineLabelResult,
  LineSettings,
  LineSummary,
  Message,
  MessageEventStreamHandlers,
  MessageReadInput,
  MessageThread,
  MobileNetworkScan,
  NetworkSelectionPolicy,
  NetworkStatus,
  OutgoingCallReservation,
  ProxyDeleteResult,
  ProxyInstance,
  ProxyMutation,
  RecordingEntry,
  RecordingSettings,
  RenameDeviceInput,
  RuntimeEventStreamHandlers,
  SaveConnectionProfileInput,
  SendMessageInput,
  SIMCommandInput,
  SIMStatus,
  SystemSettings,
  TelegramUnit,
  TelegramUnitInput,
  TLSSettings,
  UpdateCheck,
  UpdateDeviceConfigurationInput,
  UpdateGlobalCallSettingsInput,
  UpdateLineLabelInput,
  UpdateLineSettingsInput,
  UpdateMemberInput,
  UpdateSystemSettingsInput,
  UpdateNetworkSelectionInput,
  UpdateProxyInput,
  UpdateTLSSettingsInput,
  USSDCommandInput,
  USSDResponse,
  USSDStatus,
  UserAccount
} from './types'
import { ApiError, isLineColorPresetID } from './types'
import { normalizeDialTarget } from '../utils/dialTarget'
import { isIPAddress, isLoopbackAddress } from '../utils/ipAddress'
import { normalizedPhoneIdentity } from '../utils/lineIdentity'
import { proxyCredentialError } from '../utils/proxyCredentials'

const MAIN_ICCID = '8986012345678900001'
const TRAVEL_ICCID = '8984045678901230002'
const MAIN_IMSI = '001010000000001'
const TRAVEL_IMSI = '001020000000002'
const MAIN_PHONE = '+1 202 555 0101'
const TRAVEL_PHONE = '+1 202 555 0102'
const ALEX_NAME = 'Alex Rowan'
const ALEX_PHONE = '+1 202 555 0103'
const CASEY_NAME = 'Casey Morgan'
const MEMBER_PROFILE_NAME = 'Casey Park'
const CASEY_PHONE = '+1 202 555 0104'
const CASEY_WORK_PHONE = '+1 202 555 0105'
const RILEY_NAME = 'Riley Quinn'
const RILEY_PHONE = '+1 202 555 0106'
const UNKNOWN_CALLER_PHONE = '+1 202 555 0107'
const FIXTURE_APPLICATION_VERSION = 'v9.8.7'

function fixtureThreadKey(
  lineID: string,
  peer: string
): string {
  return `${lineID.trim()}|${peer.trim()}`
}

function fixtureThreadForQuery(
  query: MessageQuery | MessageReadInput
): MessageThread | undefined {
  const lineID = query.line_id.trim()
  const peer = query.peer.trim()
  return threads.find(thread => {
    return thread.line_id === lineID && thread.peer.trim() === peer
  })
}

const contacts: Contact[] = [
  {
    id: 'contact-alex',
    display_name: ALEX_NAME,
    favorite: true,
    preferred_line_id: 'line-fixture-main',
    phones: [{ id: 'phone-alex', label: 'Mobile', number: ALEX_PHONE, primary: true }],
    notes: 'Demo contact',
    revision: 3
  },
  {
    id: 'contact-casey',
    display_name: CASEY_NAME,
    favorite: false,
    preferred_line_id: 'line-fixture-travel',
    phones: [
      { id: 'phone-casey-mobile', label: 'Mobile', number: CASEY_PHONE, primary: true },
      { id: 'phone-casey-work', label: 'Work', number: CASEY_WORK_PHONE, primary: false }
    ],
    revision: 2
  },
  {
    id: 'contact-riley',
    display_name: RILEY_NAME,
    favorite: false,
    phones: [{ id: 'phone-riley', label: 'Mobile', number: RILEY_PHONE, primary: true }],
    revision: 1
  }
]

const threads: MessageThread[] = [
  {
    key: fixtureThreadKey('line-fixture-main', ALEX_PHONE),
    line_id: 'line-fixture-main',
    peer: ALEX_PHONE,
    contact_name: ALEX_NAME,
    last_timestamp: '2026-07-23T09:42:00Z',
    last_content: 'Thanks — I’ll check it later.',
    unread_count: 1,
    marked_unread: false,
    favorite: true
  },
  {
    key: fixtureThreadKey('line-fixture-travel', CASEY_PHONE),
    line_id: 'line-fixture-travel',
    peer: CASEY_PHONE,
    contact_name: CASEY_NAME,
    last_timestamp: '2026-07-22T14:18:00Z',
    last_content: 'The demo workspace is ready.',
    unread_count: 0,
    marked_unread: false,
    favorite: false
  },
  {
    key: fixtureThreadKey('line-fixture-main', RILEY_PHONE),
    line_id: 'line-fixture-main',
    peer: RILEY_PHONE,
    contact_name: RILEY_NAME,
    last_timestamp: '2026-07-20T06:05:00Z',
    last_content: 'Got it, thank you.',
    unread_count: 0,
    marked_unread: false,
    favorite: false
  }
]

const messagesByThread: Record<string, Message[]> = {
  [fixtureThreadKey('line-fixture-main', ALEX_PHONE)]: [
    {
      id: '101',
      line_id: 'line-fixture-main',
      peer: ALEX_PHONE,
      direction: 'outgoing',
      content: 'The test device is back online.',
      timestamp: '2026-07-23T09:38:00Z',
      type: 2,
      status: 2,
      delivery_status: 'delivered'
    },
    {
      id: '102',
      line_id: 'line-fixture-main',
      peer: ALEX_PHONE,
      direction: 'incoming',
      content: 'Thanks — I’ll check it later.',
      timestamp: '2026-07-23T09:42:00Z',
      type: 1,
      status: 1,
      delivery_status: ''
    }
  ],
  [fixtureThreadKey('line-fixture-travel', CASEY_PHONE)]: [
    {
      id: '103',
      line_id: 'line-fixture-travel',
      peer: CASEY_PHONE,
      direction: 'incoming',
      content: 'The demo workspace is ready.',
      timestamp: '2026-07-22T14:18:00Z',
      type: 1,
      status: 1,
      delivery_status: ''
    }
  ],
  [fixtureThreadKey('line-fixture-main', RILEY_PHONE)]: [
    {
      id: '104',
      line_id: 'line-fixture-main',
      peer: RILEY_PHONE,
      direction: 'outgoing',
      content: 'The sample report is available.',
      timestamp: '2026-07-20T05:59:00Z',
      type: 2,
      status: 2,
      delivery_status: 'submitted'
    },
    {
      id: '105',
      line_id: 'line-fixture-main',
      peer: RILEY_PHONE,
      direction: 'incoming',
      content: 'Got it, thank you.',
      timestamp: '2026-07-20T06:05:00Z',
      type: 1,
      status: 1,
      delivery_status: ''
    }
  ]
}

const calls: CallRecord[] = [
  {
    id: 'call-1',
    line_id: 'line-fixture-main',
    direction: 'incoming',
    remote_number: ALEX_PHONE,
    display_name: ALEX_NAME,
    contact_id: 'contact-alex',
    started_at: '2026-07-23T08:52:00Z',
    ended_at: '2026-07-23T08:57:12Z',
    duration_seconds: 312,
    missed: false,
    read: false,
    favorite: true
  },
  {
    id: 'call-2',
    line_id: 'line-fixture-main',
    direction: 'incoming',
    remote_number: UNKNOWN_CALLER_PHONE,
    started_at: '2026-07-22T11:14:00Z',
    ended_at: '2026-07-22T11:14:31Z',
    duration_seconds: 0,
    missed: true,
    read: false,
    favorite: false
  },
  {
    id: 'call-3',
    line_id: 'line-fixture-travel',
    direction: 'outgoing',
    remote_number: CASEY_PHONE,
    display_name: CASEY_NAME,
    contact_id: 'contact-casey',
    started_at: '2026-07-22T07:30:00Z',
    ended_at: '2026-07-22T07:33:46Z',
    duration_seconds: 226,
    missed: false,
    read: false,
    favorite: false
  }
]
const deletedRecordingIDs = new Set<string>()
const favoriteRecordingCallIDs = new Set<string>(['call-1'])

const diagnosticLogs: DiagnosticLogEntry[] = [
  {
    id: 41,
    timestamp: '2026-07-23T11:58:42Z',
    level: 'info',
    component: 'application',
    caller: 'main.go:42',
    message: 'ModemDeck service started',
    fields: { version: 'fixture', address: ':7577' }
  },
  {
    id: 42,
    timestamp: '2026-07-23T11:58:43Z',
    level: 'info',
    component: 'communications',
    caller: 'service.go:108',
    message: 'host agent snapshot loaded',
    fields: { provider: 'modemmanager', lines: 2, revision: 'fixture-revision-8' }
  },
  {
    id: 43,
    timestamp: '2026-07-23T11:59:12Z',
    level: 'warn',
    component: 'call-media',
    caller: 'runtime.go:214',
    message: 'media remains unavailable until a call is active',
    fields: { line_id: 'line-fixture-travel' }
  },
  {
    id: 44,
    timestamp: '2026-07-23T12:00:00Z',
    level: 'debug',
    component: 'http',
    caller: 'api.go:231',
    message: 'diagnostics snapshot served',
    fields: { duration: '4ms' }
  }
]

function clone<T>(value: T): T {
  return structuredClone(value)
}

function fixtureLineKey(line: LineSummary): string {
  return line.id.trim()
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
  initialIncomingCall?: boolean | 'occupied'
  initialConcurrentCalls?: boolean
  initialOutgoingReservation?: 'owned' | 'occupied'
}

function fixtureLines(count: number): LineSummary[] {
  const result: LineSummary[] = []
  if (count >= 1) {
    result.push({
      id: 'line-fixture-main',
      iccid: MAIN_ICCID,
      imsi: MAIN_IMSI,
      phone_number: MAIN_PHONE,
      operator: 'Aurora Mobile',
      home_operator_code: '00101',
      home_operator_name: 'Aurora Mobile',
      home_country_iso: 'ZZ',
      serving_operator_code: '00101',
      serving_operator_name: 'Aurora Mobile',
      serving_country_iso: 'ZZ',
      registration_state_known: true,
      registration_state_code: 1,
      registration_state: 'home',
      roaming: false,
      emergency_only: false,
      device_imei: 'fixture-001',
      device_name: 'Main cellular modem',
      line_label: 'Line A',
      line_color: 'violet',
      model: 'Fixture modem 1',
      firmware: 'Fixture 1.0',
      state: 'registered',
      radio_desired_enabled: true,
      radio_desired_enabled_known: true,
      signal_quality: 82,
      capabilities: {
        modem: true,
        sim: true,
        voice: true,
        messaging: true,
        media: true,
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
      imsi: TRAVEL_IMSI,
      phone_number: TRAVEL_PHONE,
      operator: 'Pine Wireless',
      home_operator_code: '00102',
      home_operator_name: 'Pine Wireless',
      home_country_iso: 'ZZ',
      serving_operator_code: '00101',
      serving_operator_name: 'Aurora Mobile',
      serving_country_iso: 'ZZ',
      registration_state_known: true,
      registration_state_code: 5,
      registration_state: 'roaming',
      roaming: true,
      emergency_only: false,
      device_imei: 'fixture-002',
      device_name: 'Travel cellular modem',
      line_label: 'Line B',
      line_color: 'teal',
      model: 'Fixture modem 2',
      firmware: 'Fixture 1.0',
      state: 'registered',
      radio_desired_enabled: true,
      radio_desired_enabled_known: true,
      signal_quality: 76,
      capabilities: {
        modem: true,
        sim: true,
        voice: true,
        messaging: true,
        media: false,
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
      phone_number: `+1 202 555 ${String(100 + displayIndex).padStart(4, '0')}`,
      operator: `Fixture Network ${displayIndex}`,
      home_operator_code: `001${String(displayIndex).padStart(2, '0')}`,
      home_operator_name: `Fixture Network ${displayIndex}`,
      home_country_iso: 'ZZ',
      serving_operator_code: `001${String(displayIndex).padStart(2, '0')}`,
      serving_operator_name: `Fixture Network ${displayIndex}`,
      serving_country_iso: 'ZZ',
      registration_state_known: true,
      registration_state_code: 1,
      registration_state: 'home',
      roaming: false,
      emergency_only: false,
      device_imei: `fixture-${String(displayIndex).padStart(3, '0')}`,
      device_name: `Cellular modem ${displayIndex}`,
      line_label: '',
      line_color: '',
      model: `Fixture modem ${displayIndex}`,
      firmware: 'Fixture 1.0',
      state: 'registered',
      radio_desired_enabled: true,
      radio_desired_enabled_known: true,
      signal_quality: Math.max(10, 80 - displayIndex),
      capabilities: {
        modem: true,
        sim: true,
        voice: true,
        messaging: true,
        media: displayIndex % 2 === 1,
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
    line_id: line.id,
    revision: 'fixture-revision-1',
    observed_at: '2026-07-23T12:00:00Z',
    identity: {
      manufacturer: 'Fixture',
      model: `Modem ${index + 1}`,
      firmware: 'fixture-fw-1',
      equipment_identifier: line.device_imei
    },
    details: {
      hardware_revision: `fixture-hw-${index + 1}`,
      primary_port: `cdc-wdm${index}`,
      access_technologies: index === 0 ? 1 << 14 : 1 << 5,
      snr: index === 0 ? 8.5 : null,
      ports: [
        { name: `cdc-wdm${index}`, type: 'qmi', type_code: 6 },
        { name: `ttyUSB${index * 2 + 2}`, type: 'at', type_code: 3 },
        { name: `wwan${index}`, type: 'net', type_code: 2 }
      ]
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
    automatic_apn: '',
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
    voice_verification: {
      usb_configuration: 'enabled',
      media_routing: line.capabilities?.media ? 'enabled' : 'rejected'
    },
    volte: {
      policy_known: volteAvailable,
      modem_capability_known: volteAvailable,
      modem_capability_enabled: volteAvailable,
      restart_required: false,
      provisioning: {
        backend: 'modemmanager',
        carrier_configuration: volteAvailable ? 'Fixture-VoLTE' : 'Fixture-Generic',
        carrier_configuration_reported: true,
        carrier_configuration_revision: '1',
        carrier_configuration_revision_reported: true,
        ims_profile_reported: true,
        ims_profile_present: volteAvailable
      },
      ...(volteAvailable
        ? {
            policy: 'enabled' as const,
            configuration_mode: 'forced_enabled' as const,
            profile_id: 'fixture-volte-v1'
          }
        : {})
    },
    capabilities: {
      voice: feature('modemmanager', {
        writable: false
      }),
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
        implemented: true,
        readable: true,
        writable: true
      }),
      connection_profile: feature('modemmanager', {
        implemented: true,
        readable: true,
        writable: true
      }),
      usb_reset: feature('linux_usbfs', {
        implemented: true,
        readable: true,
        writable: true
      })
    }
  }
}

export function createFixtureGateway(options: FixtureGatewayOptions = {}): ModemDeckGateway {
  const requestedLineCount = Math.max(0, Math.trunc(options.lineCount ?? 2))
  const lines = options.noDevices ? [] : fixtureLines(requestedLineCount)
  const users: UserAccount[] = [
    {
      id: 'user_admin',
      username: 'fixture',
      role: 'admin',
      enabled: true,
      must_change_password: false,
      revision: 1,
      profile_name: ALEX_NAME,
      line_ids: lines.map(line => line.id),
      created_at: '2026-07-23 12:00:00',
      updated_at: '2026-07-23 12:00:00'
    },
    {
      id: 'user_fixture_member',
      username: 'casey',
      role: 'member',
      enabled: true,
      must_change_password: false,
      revision: 2,
      profile_name: MEMBER_PROFILE_NAME,
      line_ids: lines[1] ? [lines[1].id] : [],
      created_at: '2026-07-24 12:00:00',
      updated_at: '2026-07-24 12:00:00'
    }
  ]
  if (
    (options.initialIncomingCall === 'occupied' || options.initialConcurrentCalls) &&
    lines[1]
  ) {
    lines[1].capabilities = {
      ...lines[1].capabilities,
      voice: true,
      media: true,
      dial: true
    }
  }
  const fixtureDeviceList = options.noDevices
    ? []
    : lines.map((line, index): Device => ({
        imei: line.device_imei,
        endpoint_id: line.id,
        name: line.device_name,
        model: `Fixture modem ${index + 1}`,
        firmware: 'Fixture 1.0',
        port: `cdc-wdm${index}`,
        public_ip: '',
        private_ip: index === 0 ? '10.0.0.2' : '',
        public_ipv6: '',
        private_ipv6: '',
        state: line.state,
        current_iccid: line.iccid,
        sim_inserted: true,
        signal_quality: line.signal_quality || null,
        signal_dbm: -70 - index,
        signal_rsrq: -10 - index,
        signal_rsrp: -95 - index,
        last_seen: '2026-07-23T12:00:00Z',
        present: true,
        capabilities: clone(line.capabilities || {}),
        sim: {
          iccid: line.iccid,
          imsi: line.imsi,
          phone_number: line.phone_number,
          operator: line.operator,
          home_operator_code: line.home_operator_code,
          home_operator_name: line.home_operator_name,
          home_country_iso: line.home_country_iso,
          serving_operator_code: line.serving_operator_code,
          serving_operator_name: line.serving_operator_name,
          serving_country_iso: line.serving_country_iso,
          registration_state_known: line.registration_state_known,
          registration_state_code: line.registration_state_code,
          registration_state: line.registration_state,
          roaming: line.roaming,
          current_imei: line.device_imei,
          reg_status: line.registration_state_code,
          reg_status_text: line.registration_state,
          lac: '',
          cell_id: '',
          apn: index === 0 ? 'fixture' : '',
          ims_status: index === 0 ? 1 : 0,
          last_seen: '2026-07-23T12:00:00Z'
        }
      }))
  const hardwareByLine = new Map(
    lines.map((line, index) => [line.id, fixtureHardware(line, index)])
  )
  const hardwareRevisionByLine = new Map(lines.map(line => [line.id, 1]))
  const policyByLine = new Map(
    lines.map(line => [
      line.id,
      {
        policy: 'follow_global' as IncomingCallPolicy,
        revision: 1,
        updated_at: '2026-07-23 12:00:00'
      }
    ])
  )
  const messagePolicyByLine = new Map(
    lines.map(line => [
      line.id,
      {
        delivery_reports_enabled: false,
        delivery_reports_support: 'unknown' as const,
        revision: 1
      }
    ])
  )
  const networkSelectionByLine = new Map<string, NetworkSelectionPolicy>(
    lines.map((line, index) => [
      fixtureLineKey(line),
      {
        line_id: fixtureLineKey(line),
        mode: index === 1 ? 'manual' : 'auto',
        ...(index === 1 ? { operator_code: '00101' } : {}),
        revision: 1,
        applied: true,
        applied_at: '2026-07-23T12:00:00Z'
      }
    ])
  )
  let sequence = 200
  let globalCallSettings: GlobalCallSettings = {
    receive_calls: true,
    revision: 1
  }
  let lineSettings: LineSettings = {
    default_line_id: lines[0] ? fixtureLineKey(lines[0]) : '',
    revision: 1
  }
  let systemSettings: SystemSettings = {
    language: 'auto',
    revision: 1
  }
  const connectionProfiles = new Map<string, ConnectionProfile[]>(
    lines.map(line => [
      fixtureLineKey(line),
      [
        {
          profile_id: 1,
          profile_name: 'ims',
          apn: 'ims',
          ip_family: 'ipv4v6',
          ip_type: 3,
          apn_type: 2,
          allowed_auth: 0,
          access_type_preference: 0,
          roaming_allowance: 0,
          profile_source: 0
        },
        {
          profile_id: 2,
          profile_name: 'data',
          apn: '',
          ip_family: 'ipv4v6',
          ip_type: 3,
          apn_type: 1,
          allowed_auth: 0,
          access_type_preference: 0,
          roaming_allowance: 0,
          profile_source: 0
        }
      ]
    ])
  )
  const initialCallTimestamp = new Date().toISOString()
  let activeCall: CallSession | undefined =
    options.initialConcurrentCalls && lines[0]
      ? {
          id: 'call-fixture-concurrent-main',
          line_id: fixtureLineKey(lines[0]),
          direction: 'outgoing',
          remote_number: ALEX_PHONE,
          display_name: ALEX_NAME,
          phase: 'active',
          control_state: 'occupied',
          media_available: false,
          created_at: initialCallTimestamp,
          active_at: initialCallTimestamp,
          bearer: 'volte'
        }
      : options.initialIncomingCall && lines[0]
      ? {
          id: 'call-fixture-incoming',
          line_id: fixtureLineKey(lines[0]),
          direction: 'incoming',
          remote_number: ALEX_PHONE,
          display_name: ALEX_NAME,
          phase: options.initialIncomingCall === 'occupied' ? 'active' : 'ringing',
          control_state:
            options.initialIncomingCall === 'occupied' ? 'occupied' : 'available',
          media_available: false,
          created_at: initialCallTimestamp,
          ...(options.initialIncomingCall === 'occupied'
            ? {
                active_at: initialCallTimestamp,
                bearer: 'volte'
              }
            : {})
        }
      : undefined
  const additionalActiveCalls: CallSession[] =
    options.initialConcurrentCalls && lines[1]
      ? [
          {
            id: 'call-fixture-concurrent-secondary',
            line_id: fixtureLineKey(lines[1]),
            direction: 'incoming',
            remote_number: CASEY_PHONE,
            display_name: CASEY_NAME,
            phase: 'active',
            control_state: 'occupied',
            media_available: false,
            created_at: initialCallTimestamp,
            active_at: initialCallTimestamp,
            bearer: 'volte'
          }
        ]
      : []
  const outgoingReservations: OutgoingCallReservation[] =
    options.initialOutgoingReservation && lines[0]
      ? [
          {
            request_id: 'request-fixture-outgoing',
            line_id: fixtureLineKey(lines[0]),
            control_state: options.initialOutgoingReservation,
            created_at: new Date().toISOString()
          }
        ]
      : []
  let activeCallRecording: CallRecordingState | undefined = activeCall
    ? {
        call_id: activeCall.id,
        enabled: false,
        active: false
      }
    : undefined
  let activeCallRecordingSegments: CallRecordingSegment[] = []
  let activeCallRecordingSequence = 0

  function startFixtureRecordingSegment(): void {
    if (!activeCall || activeCallRecordingSegments.some(segment => segment.status === 'recording')) {
      return
    }
    activeCallRecordingSequence += 1
    const startedAt = new Date().toISOString()
    activeCallRecordingSegments.push({
      id: `recording-${activeCall.id}-${activeCallRecordingSequence}`,
      call_id: activeCall.id,
      segment_index: activeCallRecordingSequence,
      status: 'recording',
      recorded_at: startedAt,
      started_at: startedAt,
      duration_seconds: 0,
      size_bytes: 0,
      playable: false
    })
  }

  function finishFixtureRecordingSegment(): void {
    const segment = activeCallRecordingSegments.find(item => item.status === 'recording')
    if (!segment) return
    const endedAt = new Date().toISOString()
    segment.status = 'ready'
    segment.ended_at = endedAt
    segment.duration_seconds = Math.max(
      1,
      Math.floor((Date.parse(endedAt) - Date.parse(segment.started_at || endedAt)) / 1000)
    )
    segment.size_bytes = 16_044
    segment.playable = true
    segment.content_type = 'audio/wav'
    segment.download_url = recordingFixtureURL()
  }
  let callPolls = 0
  let recordingSettings: RecordingSettings = {
    default_enabled: false,
    revision: 1
  }
  let tlsSettings: TLSSettings = {
    mode: 'automatic',
    subject: 'CN=modemdeck.local',
    issuer: 'ModemDeck Local CA',
    dns_names: ['modemdeck.local', 'gateway.modemdeck.local'],
    ip_addresses: ['192.168.1.10'],
    not_before: '2026-07-01T00:00:00Z',
    not_after: '2026-09-29T23:59:59Z',
    fingerprint_sha256:
      '75:8A:8F:23:1C:9B:43:D7:5F:6E:41:65:14:29:CC:20:B1:E6:C9:8A:C7:31:58:5D:D0:19:BE:02:C3:7A:E4:91',
    expired: false,
    renews_automatically: true
  }
  const telegramUnits: TelegramUnit[] = [
    {
      id: 'telegram-main',
      display_name: '主线路通知',
      enabled: true,
      chat_id: '-1001234567890',
      admin_id: '100000001',
      assigned_user_id: 'user_fixture_member',
      assigned_username: 'casey',
      all_assigned_lines: true,
      effective_enabled: true,
      line_scopes: [],
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
      assigned_user_id: 'user_admin',
      assigned_username: 'fixture',
      all_assigned_lines: false,
      effective_enabled: false,
      line_scopes: ['line-fixture-travel'],
      incoming_sms: true,
      missed_calls: false,
      token_configured: true,
      bot_username: 'modemdeck_travel_bot',
      revision: 2
    }
  ]
  function resolvedFixtureTelegramUnit(unit: TelegramUnit): TelegramUnit {
    const assignedUser = users.find(user => user.id === unit.assigned_user_id)
    const assignedLineIDs = new Set(assignedUser?.line_ids || [])
    return {
      ...unit,
      assigned_username: assignedUser?.username || '',
      effective_enabled: unit.enabled && assignedUser?.enabled === true,
      line_scopes: unit.all_assigned_lines
        ? [...assignedLineIDs]
        : unit.line_scopes.filter(lineID => assignedLineIDs.has(lineID))
    }
  }
  const proxyPasswords = new Set<string>()
  const proxies: ProxyInstance[] = lines.slice(0, 2).map((line, index) => {
    const secured = index === 1
    const id = `proxy-fixture-${index + 1}`
    if (secured) proxyPasswords.add(id)
    return {
      id,
      name: `${line.line_label || `线路 ${index + 1}`} ${secured ? 'HTTP' : 'SOCKS5'} ${secured ? 3128 : 1080}`,
      line_id: fixtureLineKey(line),
      enabled: true,
      mode: secured ? 'http' : 'socks5',
      listen_address: secured ? '0.0.0.0' : '127.0.0.1',
      listen_port: secured ? 3128 : 1080,
      auth_enabled: secured,
      username: secured ? 'deck' : '',
      has_password: secured,
      revision: 1,
      applied_revision: 1,
      apply_state: 'applied',
      created_at: '2026-07-23T12:00:00Z',
      updated_at: '2026-07-23T12:00:00Z'
    }
  })

  function fixtureLineConnected(lineID: string): boolean {
    return lines.findIndex(line => fixtureLineKey(line) === lineID) === 0
  }

  function fixtureNetworkStatus(): NetworkStatus {
    const networkLines = lines.map((line, index) => ({
      line_id: fixtureLineKey(line),
      connected: index === 0,
      interface: index === 0 ? 'wwan0' : '',
      addresses: index === 0 ? ['192.0.2.10', '2001:db8::10'] : [],
      dns: index === 0 ? ['1.1.1.1', '8.8.8.8'] : [],
      rx_bytes: index === 0 ? 4_820_001_423 : 736_010_442,
      tx_bytes: index === 0 ? 682_040_112 : 95_100_882,
      error: ''
    }))
    const runtimeProxies = proxies.map((proxy, index) => {
      const lineExists = lines.some(line => fixtureLineKey(line) === proxy.line_id)
      const connected = fixtureLineConnected(proxy.line_id)
      const state = !proxy.enabled
        ? 'disabled'
        : !lineExists
          ? 'error'
          : connected
            ? 'running'
            : 'waiting_for_bearer'
      return {
        id: proxy.id,
        line_id: proxy.line_id,
        state,
        running: state === 'running',
        mode: proxy.mode,
        listen_address: proxy.listen_address,
        listen_port: proxy.listen_port,
        interface: state === 'running' ? 'wwan0' : '',
        runtime_epoch: state === 'running' ? `fixture-runtime-${proxy.revision}` : '',
        started_at: state === 'running' ? '2026-07-23T12:00:00Z' : undefined,
        bytes_up: state === 'running' ? 14_200_000 + index * 2_000_000 : 0,
        bytes_down: state === 'running' ? 184_000_000 + index * 8_000_000 : 0,
        connections: state === 'running' ? 82 + index * 11 : 0,
        active_connections: state === 'running' ? 3 + index : 0,
        last_error: state === 'error' ? 'configured line is unavailable' : ''
      } satisfies NetworkStatus['proxies'][number]
    })
    return {
      available: true,
      state: 'available',
      boot_epoch: 'fixture-network-boot',
      observed_at: '2026-07-23T12:00:00Z',
      lines: networkLines,
      proxies: runtimeProxies,
      today_total: {
        rx_bytes: 2_182_000_000,
        tx_bytes: 269_000_000
      },
      today_usage: [
        ...lines.map((line, index) => ({
          scope_kind: 'line' as const,
          scope_id: fixtureLineKey(line),
          rx_bytes: index === 0 ? 1_842_000_000 : 340_000_000,
          tx_bytes: index === 0 ? 224_000_000 : 45_000_000
        })),
        ...proxies.map((proxy, index) => ({
          scope_kind: 'proxy' as const,
          scope_id: proxy.id,
          rx_bytes: 184_000_000 + index * 8_000_000,
          tx_bytes: 14_200_000 + index * 2_000_000
        }))
      ],
      month_total: {
        rx_bytes: 30_880_000_000,
        tx_bytes: 4_370_000_000
      },
      month_usage: [
        ...lines.map((line, index) => ({
          scope_kind: 'line' as const,
          scope_id: fixtureLineKey(line),
          rx_bytes: index === 0 ? 22_640_000_000 : 8_240_000_000,
          tx_bytes: index === 0 ? 3_240_000_000 : 1_130_000_000
        })),
        ...proxies.map((proxy, index) => ({
          scope_kind: 'proxy' as const,
          scope_id: proxy.id,
          rx_bytes: 2_840_000_000 + index * 80_000_000,
          tx_bytes: 620_000_000 + index * 20_000_000
        }))
      ],
      stale: false,
      apply_pending: false,
      apply_status: 'applied',
      apply_attempts: 0,
      apply_exhausted: false
    }
  }

  function fixtureProxyError(message: string, field = ''): ApiError {
    return new ApiError(message, 400, 'invalid_argument', field)
  }

  function validateFixtureProxy(
    input: CreateProxyInput | UpdateProxyInput,
    existing?: ProxyInstance
  ): void {
    if (!lines.some(line => fixtureLineKey(line) === input.line_id)) {
      throw fixtureProxyError('线路不存在', 'line_id')
    }
    if (input.mode !== 'http' && input.mode !== 'socks5') {
      throw fixtureProxyError('代理协议无效', 'mode')
    }
    if (!isIPAddress(input.listen_address)) {
      throw fixtureProxyError('监听地址必须是 IPv4 或 IPv6 地址', 'listen_address')
    }
    if (
      !Number.isSafeInteger(input.listen_port) ||
      input.listen_port < 1024 ||
      input.listen_port > 65535
    ) {
      throw fixtureProxyError('监听端口无效', 'listen_port')
    }
    if (!isLoopbackAddress(input.listen_address) && !input.auth_enabled) {
      throw fixtureProxyError('非本机监听必须启用认证', 'auth_enabled')
    }
    const credentialError = proxyCredentialError(
      input.mode,
      input.username,
      input.password || '',
      existing?.has_password
    )
    if (credentialError) {
      const field = credentialError.includes('用户名') ? 'username' : 'password'
      throw fixtureProxyError(credentialError, field)
    }
    if (
      input.auth_enabled &&
      (!input.username.trim() || (!input.password && !existing?.has_password))
    ) {
      throw fixtureProxyError('认证需要用户名和密码', 'password')
    }
  }

  function configurationForLine(lineID: string): DeviceConfiguration {
    const line = lines.find(candidate => candidate.id === lineID)
    const hardware = hardwareByLine.get(lineID)
    const policy = policyByLine.get(lineID)
    const messagePolicy = messagePolicyByLine.get(lineID)
    if (!line || !hardware || !policy || !messagePolicy) {
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
      },
      messaging: clone(messagePolicy)
    }
  }

  function advanceHardwareRevision(lineID: string, hardware: DeviceHardwareConfiguration): void {
    const revision = (hardwareRevisionByLine.get(lineID) || 1) + 1
    hardwareRevisionByLine.set(lineID, revision)
    hardware.revision = `fixture-revision-${revision}`
    hardware.observed_at = '2026-07-23T12:01:00Z'
  }

  function filteredDiagnosticLogs(query: DiagnosticLogQuery = {}): DiagnosticLogPage {
    const level = query.level || ''
    const component = query.component?.trim().toLocaleLowerCase() || ''
    const search = query.search?.trim().toLocaleLowerCase() || ''
    const after = query.after || 0
    const limit = Math.max(1, Math.min(2000, Math.trunc(query.limit || 500)))
    const matching = diagnosticLogs.filter(entry => {
      if (entry.id <= after) return false
      if (level && entry.level !== level) return false
      if (component && entry.component.toLocaleLowerCase() !== component) return false
      if (!search) return true
      return [
        entry.message,
        entry.component,
        entry.caller || '',
        JSON.stringify(entry.fields || {})
      ].some(value => value.toLocaleLowerCase().includes(search))
    })
    return {
      entries: clone(matching.slice(-limit)),
      oldest_id: diagnosticLogs[0]?.id || 0,
      newest_id: diagnosticLogs.at(-1)?.id || 0,
      truncated: matching.length > limit || (after > 0 && after + 1 < (diagnosticLogs[0]?.id || 0))
    }
  }

  return {
    async getAbout(): Promise<AboutInfo> {
      return {
        name: 'ModemDeck',
        version: FIXTURE_APPLICATION_VERSION,
        repository_url: 'https://github.com/human-agent65535/ModemDeck',
        license_name: 'PolyForm Noncommercial 1.0.0',
        license_url: 'https://github.com/human-agent65535/ModemDeck/blob/modemdeck/LICENSE',
        notices_url:
          'https://github.com/human-agent65535/ModemDeck/blob/modemdeck/THIRD_PARTY_NOTICES.md'
      }
    },

    async checkForUpdates(): Promise<UpdateCheck> {
      return {
        status: 'unavailable',
        current_version: FIXTURE_APPLICATION_VERSION,
        checked_at: new Date().toISOString(),
        error_code: 'github_no_release'
      }
    },

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
        lines: clone(lines),
        line_catalog: clone(lines),
        line_settings: clone(lineSettings),
        system_settings: clone(systemSettings)
      }
    },

    async listUsers(): Promise<UserAccount[]> {
      return clone(users)
    },

    async createMember(input: CreateMemberInput): Promise<UserAccount> {
      if (users.some(user => user.username.toLowerCase() === input.username.toLowerCase())) {
        throw new ApiError('Username is already in use', 409, 'username_conflict')
      }
      sequence += 1
      const user: UserAccount = {
        id: `user_fixture_${sequence}`,
        username: input.username,
        role: 'member',
        enabled: true,
        must_change_password: true,
        revision: 1,
        line_ids: [...input.line_ids],
        created_at: '2026-07-29 12:00:00',
        updated_at: '2026-07-29 12:00:00'
      }
      users.push(user)
      return clone(user)
    },

    async updateMember(id: string, input: UpdateMemberInput): Promise<UserAccount> {
      const index = users.findIndex(user => user.id === id)
      const current = users[index]
      if (!current) throw new ApiError('User was not found', 404, 'user_not_found')
      if (current.revision !== input.revision) {
        throw new ApiError('User changed since it was loaded', 409, 'revision_conflict')
      }
      if (
        current.role === 'admin' &&
        (input.username !== current.username || !input.enabled)
      ) {
        throw new ApiError('User data is invalid', 400, 'invalid_user')
      }
      const updated: UserAccount = {
        ...current,
        username: input.username,
        enabled: input.enabled,
        line_ids: [...input.line_ids],
        revision: current.revision + 1,
        updated_at: '2026-07-29 12:01:00'
      }
      users[index] = updated
      return clone(updated)
    },

    async setMemberPassword(id: string, password: string): Promise<void> {
      const user = users.find(candidate => candidate.id === id && candidate.role === 'member')
      if (!user) throw new ApiError('User was not found', 404, 'user_not_found')
      if (new TextEncoder().encode(password).length < 12) {
        throw new ApiError('Password is too short', 422, 'password_too_short')
      }
      user.must_change_password = false
      user.revision += 1
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
        avatar: input.avatar,
        favorite: input.favorite,
        notes: input.notes,
        preferred_line_id: input.preferred_line_id,
        revision: 1,
        phones: input.phones.map((phone, index) => ({
          id: `phone-fixture-${sequence}-${index + 1}`,
          label: phone.label,
          number: phone.number,
          region: phone.region,
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
        avatar: input.avatar,
        favorite: input.favorite,
        notes: input.notes,
        preferred_line_id: input.preferred_line_id,
        revision: (current.revision || 0) + 1,
        phones: input.phones.map((phone, phoneIndex) => ({
          id: phone.id || `phone-fixture-${sequence}-${phoneIndex + 1}`,
          label: phone.label,
          number: phone.number,
          region: phone.region,
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

    async deleteContacts(items): Promise<void> {
      for (const item of items) {
        const index = contacts.findIndex(contact => contact.id === item.id)
        if (index < 0) throw new ApiError('联系人不存在', 404)
        contacts.splice(index, 1)
      }
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
      const thread = fixtureThreadForQuery(query)
      return clone((thread && messagesByThread[thread.key]) || [])
    },

    subscribeMessageEvents(_handlers: MessageEventStreamHandlers): () => void {
      return () => undefined
    },

    subscribeRuntimeEvents(_handlers: RuntimeEventStreamHandlers): () => void {
      return () => undefined
    },

    async markThreadRead(query: MessageReadInput): Promise<void> {
      const thread = fixtureThreadForQuery(query)
      if (thread) {
        thread.unread_count = 0
        thread.marked_unread = false
      }
    },

    async updateMessageThreads(action, inputs): Promise<void> {
      for (const input of inputs) {
        const thread = fixtureThreadForQuery(input)
        if (!thread) throw new ApiError('短信会话不存在', 404)
        switch (action) {
          case 'read':
            thread.unread_count = 0
            thread.marked_unread = false
            break
          case 'unread':
            thread.marked_unread = true
            break
          case 'favorite':
            thread.favorite = true
            break
          case 'unfavorite':
            thread.favorite = false
            break
          case 'delete': {
            const index = threads.findIndex(item => item.key === thread.key)
            if (index >= 0) threads.splice(index, 1)
            delete messagesByThread[thread.key]
            break
          }
        }
      }
    },

    async deleteThread(query: MessageReadInput): Promise<void> {
      const thread = fixtureThreadForQuery(query)
      if (!thread) throw new ApiError('短信会话不存在', 404)
      const index = threads.findIndex(item => item.key === thread.key)
      if (index >= 0) threads.splice(index, 1)
      delete messagesByThread[thread.key]
    },

    async sendMessage(input: SendMessageInput): Promise<Message> {
      sequence += 1
      const line = lines.find(item => fixtureLineKey(item) === input.line_id)
      if (!line) throw new ApiError('请选择线路', 400)
      const key = fixtureThreadKey(input.line_id, input.to)
      let thread = threads.find(item => item.key === key)
      if (!thread) {
        const contact = contacts.find(item =>
          item.phones.some(
            phone =>
              normalizedPhoneIdentity(phone.normalized_number || phone.number) ===
              normalizedPhoneIdentity(input.to)
          )
        )
        thread = {
          key,
          line_id: input.line_id,
          peer: input.to,
          contact_name: contact?.display_name,
          last_timestamp: '2026-07-23T12:00:00Z',
          unread_count: 0,
          marked_unread: false,
          favorite: false
        }
        threads.unshift(thread)
        messagesByThread[key] = []
      }
      const message: Message = {
        id: String(sequence),
        line_id: thread.line_id,
        peer: input.to,
        direction: 'outgoing',
        content: input.content,
        timestamp: '2026-07-23T12:00:00Z',
        type: 2,
        status: 2,
        delivery_status: 'submitted'
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

    async markMissedCallsRead(): Promise<void> {
      for (const call of calls) {
        if (call.missed) call.read = true
      }
    },

    async markMissedCallRead(id: string): Promise<void> {
      const call = calls.find(item => item.id === id)
      if (!call) throw new ApiError('通话记录不存在', 404)
      if (call.missed) call.read = true
    },

    async updateCalls(action, ids): Promise<void> {
      for (const id of ids) {
        const index = calls.findIndex(call => call.id === id)
        if (index < 0) throw new ApiError('通话记录不存在', 404)
        const call = calls[index]
        if (!call) continue
        if (action === 'delete') calls.splice(index, 1)
        else if (action === 'favorite' || action === 'unfavorite') {
          call.favorite = action === 'favorite'
        } else if (call.missed) call.read = action === 'read'
      }
    },

    async deleteCall(id: string): Promise<void> {
      const index = calls.findIndex(call => call.id === id)
      if (index < 0) throw new ApiError('通话记录不存在', 404)
      calls.splice(index, 1)
    },

    async getActiveCallSnapshot(): Promise<ActiveCallSnapshot> {
      if (!activeCall || activeCall.phase === 'ended' || activeCall.phase === 'failed') {
        return {
          calls: clone(additionalActiveCalls),
          reservations: clone(outgoingReservations)
        }
      }
      if (activeCall.direction === 'outgoing') {
        callPolls += 1
        if (activeCall.phase === 'dialing' && callPolls >= 1) activeCall.phase = 'ringing'
        else if (activeCall.phase === 'ringing' && callPolls >= 2) {
          activeCall.phase = 'active'
          activeCall.active_at = '2026-07-23T12:05:04Z'
          activeCall.bearer = 'volte'
          if (activeCallRecording?.enabled) {
            activeCallRecording.active = true
            activeCallRecording.started_at = new Date().toISOString()
            startFixtureRecordingSegment()
          }
        }
      }
      return {
        calls: [clone(activeCall), ...clone(additionalActiveCalls)],
        reservations: clone(outgoingReservations)
      }
    },

    async startCall(
      lineKey: string,
      number: string,
      recordingEnabled?: boolean
    ): Promise<CallSession> {
      const dialTarget = normalizeDialTarget(number)
      if (dialTarget.error) {
        throw new ApiError('Dial target is invalid', 400, 'invalid_argument', 'number')
      }
      sequence += 1
      callPolls = 0
      const contact = contacts.find(item =>
        item.phones.some(
          phone =>
            normalizedPhoneIdentity(phone.normalized_number || phone.number) ===
            normalizedPhoneIdentity(dialTarget.normalized)
        )
      )
      activeCall = {
        id: `call-fixture-${sequence}`,
        line_id: lineKey,
        direction: 'outgoing',
        remote_number: dialTarget.normalized,
        display_name: contact?.display_name,
        phase: 'dialing',
        control_state: 'owned',
        media_available: false,
        created_at: '2026-07-23T12:05:00Z'
      }
      activeCallRecording = {
        call_id: activeCall.id,
        enabled: recordingEnabled ?? recordingSettings.default_enabled,
        active: false
      }
      activeCallRecordingSegments = []
      activeCallRecordingSequence = 0
      return clone(activeCall)
    },

    async callAction(id: string, action: 'answer' | 'reject' | 'hangup'): Promise<void> {
      if (!activeCall || activeCall.id !== id) throw new ApiError('通话不存在', 404)
      if (action === 'answer') {
        activeCall.phase = 'active'
        activeCall.control_state = 'owned'
        activeCall.active_at = '2026-07-23T12:05:04Z'
        activeCall.bearer = 'volte'
        if (activeCallRecording?.enabled) {
          activeCallRecording.active = true
          activeCallRecording.started_at = new Date().toISOString()
          startFixtureRecordingSegment()
        }
      } else {
        finishFixtureRecordingSegment()
        activeCall.phase = 'ended'
        activeCall.ended_at = '2026-07-23T12:08:00Z'
        if (activeCallRecording) activeCallRecording.active = false
      }
    },

    async sendDTMF(id: string, digit: string): Promise<void> {
      if (!activeCall || activeCall.id !== id) throw new ApiError('通话不存在', 404)
      if (activeCall.phase !== 'active') throw new ApiError('通话尚未接通', 409)
      if (!/^[0-9*#A-D]$/.test(digit)) throw new ApiError('DTMF 按键无效', 400)
    },

    async renewCallLease(id: string) {
      if (!activeCall || activeCall.id !== id) throw new ApiError('通话不存在', 404)
      return {
        call_id: id,
        holder_id: 'fixture-browser',
        expires_at: new Date(Date.now() + 15_000).toISOString()
      }
    },

    async exchangeCallMedia(): Promise<string> {
      throw new ApiError('测试数据未连接音频设备', 503)
    },

    async releaseCallMedia(): Promise<void> {
      return undefined
    },

    async listRecordings(query: ListQuery = {}): Promise<RecordingEntry[]> {
      const q = normalizedQuery(query)
      return clone(
        calls
          .filter(call => call.id === 'call-1' || call.id === 'call-3')
          .filter(call => !deletedRecordingIDs.has(`recording-${call.id}`))
          .filter(
            call =>
              !q ||
              includes(call.display_name, q) ||
              includes(call.remote_number, q)
          )
          .map(call => {
            const recordedAt =
              call.id === 'call-1'
                ? '2026-07-23T08:52:04Z'
                : '2026-07-22T07:30:03Z'
            return {
              id: `recording-${call.id}`,
              call_id: call.id,
              segment_index: 1,
              status: 'ready' as const,
              recorded_at: recordedAt,
              started_at: recordedAt,
              ended_at:
                call.id === 'call-1'
                  ? '2026-07-23T08:52:05Z'
                  : '2026-07-22T07:30:04Z',
              duration_seconds: 1,
              size_bytes: 16044,
              playable: true,
              favorite: favoriteRecordingCallIDs.has(call.id),
              content_type: 'audio/wav',
              download_url: recordingFixtureURL(),
              call
            }
          })
      )
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

    async getTLSSettings(): Promise<TLSSettings> {
      return clone(tlsSettings)
    },

    async updateTLSSettings(input: UpdateTLSSettingsInput): Promise<TLSSettings> {
      if (input.operation === 'use_automatic') {
        tlsSettings = {
          mode: 'automatic',
          subject: 'CN=modemdeck.local',
          issuer: 'ModemDeck Local CA',
          dns_names: ['modemdeck.local', 'gateway.modemdeck.local'],
          ip_addresses: ['192.168.1.10'],
          not_before: '2026-07-01T00:00:00Z',
          not_after: '2026-09-29T23:59:59Z',
          fingerprint_sha256:
            '75:8A:8F:23:1C:9B:43:D7:5F:6E:41:65:14:29:CC:20:B1:E6:C9:8A:C7:31:58:5D:D0:19:BE:02:C3:7A:E4:91',
          expired: false,
          renews_automatically: true
        }
        return clone(tlsSettings)
      }
      if (!input.certificate_pem.trim() || !input.private_key_pem.trim()) {
        throw new ApiError('证书和私钥不能为空', 400)
      }
      tlsSettings = {
        mode: 'user',
        subject: 'CN=uploaded.modemdeck.local',
        issuer: 'ModemDeck Fixture CA',
        dns_names: ['uploaded.modemdeck.local'],
        ip_addresses: ['192.168.1.10'],
        not_before: '2026-07-24T00:00:00Z',
        not_after: '2027-07-24T00:00:00Z',
        fingerprint_sha256:
          'A4:19:3C:C2:F8:67:70:B1:05:55:7D:88:9F:00:0C:D6:2A:09:2E:58:90:3C:D9:50:B2:D8:C6:31:AF:B0:6E:42',
        expired: false,
        renews_automatically: false
      }
      return clone(tlsSettings)
    },

    async setCallRecording(id: string, enabled: boolean): Promise<CallRecordingState> {
      if (!activeCall || activeCall.id !== id || !activeCallRecording) {
        throw new ApiError('通话不存在', 404)
      }
      const wasActive = activeCallRecording.active
      activeCallRecording.enabled = enabled
      activeCallRecording.active = enabled && activeCall.phase === 'active'
      activeCallRecording.error = undefined
      activeCallRecording.started_at = activeCallRecording.active
        ? wasActive
          ? activeCallRecording.started_at
          : new Date().toISOString()
        : undefined
      if (wasActive && !activeCallRecording.active) finishFixtureRecordingSegment()
      if (!wasActive && activeCallRecording.active) startFixtureRecordingSegment()
      return clone(activeCallRecording)
    },

    async listCallRecordings(id: string): Promise<CallRecordingSegment[]> {
      if (activeCall?.id === id) return clone(activeCallRecordingSegments)
      if (!calls.some(call => call.id === id)) throw new ApiError('通话记录不存在', 404)
      if (
        (id !== 'call-1' && id !== 'call-3') ||
        deletedRecordingIDs.has(`recording-${id}`)
      ) return []
      return [
        {
          id: `recording-${id}`,
          call_id: id,
          segment_index: 1,
          status: 'ready',
          recorded_at:
            id === 'call-1' ? '2026-07-23T08:52:04Z' : '2026-07-22T07:30:03Z',
          started_at: id === 'call-1' ? '2026-07-23T08:52:04Z' : '2026-07-22T07:30:03Z',
          ended_at: id === 'call-1' ? '2026-07-23T08:52:05Z' : '2026-07-22T07:30:04Z',
          duration_seconds: 1,
          playable: true,
          content_type: 'audio/wav',
          size_bytes: 16044,
          download_url: recordingFixtureURL()
        }
      ]
    },

    async deleteRecording(callID: string, recordingID: string): Promise<void> {
      if (!calls.some(call => call.id === callID)) throw new ApiError('通话记录不存在', 404)
      if (recordingID !== `recording-${callID}` || deletedRecordingIDs.has(recordingID)) {
        throw new ApiError('录音不存在', 404)
      }
      deletedRecordingIDs.add(recordingID)
    },

    async updateRecordings(action, recordings): Promise<void> {
      for (const recording of recordings) {
        if (!calls.some(call => call.id === recording.call_id)) {
          throw new ApiError('通话记录不存在', 404)
        }
        if (action === 'delete') {
          deletedRecordingIDs.add(recording.id)
        } else if (action === 'favorite') {
          favoriteRecordingCallIDs.add(recording.call_id)
        } else {
          favoriteRecordingCallIDs.delete(recording.call_id)
        }
      }
    },

    async listDevices(): Promise<Device[]> {
      return clone(fixtureDeviceList)
    },

    async createDevice(input: CreateDeviceInput): Promise<Device> {
      const imei = input.imei.trim()
      if (!imei) throw new ApiError('IMEI 不能为空', 400, 'invalid_device')
      if (fixtureDeviceList.some(device => device.imei === imei)) {
        throw new ApiError('设备已存在', 409, 'device_exists')
      }
      const device: Device = {
        imei,
        endpoint_id: '',
        name: input.name?.trim() || '',
        model: '',
        firmware: '',
        port: '',
        public_ip: '',
        private_ip: '',
        public_ipv6: '',
        private_ipv6: '',
        current_iccid: '',
        sim_inserted: false,
        signal_quality: null,
        signal_dbm: null,
        signal_rsrq: null,
        signal_rsrp: null,
        present: false
      }
      fixtureDeviceList.push(device)
      return clone(device)
    },

    async renameDevice(imei: string, input: RenameDeviceInput): Promise<Device> {
      const device = fixtureDeviceList.find(item => item.imei === imei)
      if (!device) throw new ApiError('设备不存在', 404, 'device_not_found')
      device.name = input.name.trim()
      return clone(device)
    },

    async deleteDevice(imei: string): Promise<void> {
      const index = fixtureDeviceList.findIndex(item => item.imei === imei)
      if (index < 0) throw new ApiError('设备不存在', 404, 'device_not_found')
      if (fixtureDeviceList[index]?.present) {
        throw new ApiError('已连接的模组不能删除', 409, 'device_present')
      }
      fixtureDeviceList.splice(index, 1)
    },

    async updateLineLabel(lineID: string, input: UpdateLineLabelInput): Promise<LineLabelResult> {
      const normalizedLineID = lineID.trim()
      const line = lines.find(item => fixtureLineKey(item) === normalizedLineID)
      if (!line) throw new ApiError('线路不存在', 404, 'line_not_found')
      const label = input.line_label.trim()
      if (Array.from(label).length > 16) {
        throw new ApiError('线路标签不能超过 16 个字符', 400, 'invalid_line_label')
      }
      if (input.line_color !== undefined && !isLineColorPresetID(input.line_color)) {
        throw new ApiError('线路标签颜色无效', 400, 'invalid_line_color')
      }
      line.line_label = label
      if (input.line_color !== undefined) line.line_color = input.line_color
      return {
        line_id: fixtureLineKey(line),
        line_label: line.line_label,
        line_color: line.line_color || ''
      }
    },

    async getNetworkStatus(): Promise<NetworkStatus> {
      return clone(fixtureNetworkStatus())
    },

    async getNetworkSelection(lineID: string): Promise<NetworkSelectionPolicy> {
      const policy = networkSelectionByLine.get(lineID)
      if (!policy) throw new ApiError('线路不存在', 404, 'not_found')
      return clone(policy)
    },

    async updateNetworkSelection(
      lineID: string,
      input: UpdateNetworkSelectionInput
    ): Promise<NetworkSelectionPolicy> {
      const policy = networkSelectionByLine.get(lineID)
      if (!policy) throw new ApiError('线路不存在', 404, 'not_found')
      if (input.expected_revision !== policy.revision) {
        throw new ApiError('网络选择设置已被其他会话修改', 409, 'conflict')
      }
      const operatorCode = input.operator_code?.trim() || ''
      if (input.mode === 'manual' && !/^\d{5,6}$/.test(operatorCode)) {
        throw new ApiError('请选择运营商', 400, 'invalid_operator')
      }
      const updated: NetworkSelectionPolicy = {
        line_id: lineID,
        mode: input.mode,
        ...(input.mode === 'manual' ? { operator_code: operatorCode } : {}),
        revision: policy.revision + 1,
        applied: true,
        applied_at: '2026-07-23T12:01:00Z'
      }
      networkSelectionByLine.set(lineID, updated)
      return clone(updated)
    },

    async scanMobileNetworks(
      lineID: string,
      signal?: AbortSignal
    ): Promise<MobileNetworkScan> {
      signal?.throwIfAborted()
      if (!networkSelectionByLine.has(lineID)) {
        throw new ApiError('线路不存在', 404, 'not_found')
      }
      return {
        line_id: lineID,
        observed_at: '2026-07-23T12:02:00Z',
        networks: [
          {
            status: 'current',
            operator_code: '00101',
            operator_long: 'Aurora Mobile',
            operator_short: 'Aurora',
            access_technologies: 1 << 14,
            access_technology_names: ['LTE']
          },
          {
            status: 'available',
            operator_code: '00102',
            operator_long: 'Pine Wireless',
            operator_short: 'Pine',
            access_technologies: 1 << 14,
            access_technology_names: ['LTE']
          },
          {
            status: 'forbidden',
            operator_code: '44020',
            operator_long: 'SoftBank',
            operator_short: 'SoftBank',
            access_technologies: 1 << 14,
            access_technology_names: ['LTE']
          }
        ]
      }
    },

    async listProxies(): Promise<ProxyInstance[]> {
      return clone(proxies)
    },

    async createProxy(input: CreateProxyInput): Promise<ProxyMutation> {
      validateFixtureProxy(input)
      sequence += 1
      const id = `proxy-fixture-${sequence}`
      if (input.auth_enabled) proxyPasswords.add(id)
      const proxy: ProxyInstance = {
        id,
        name: input.name.trim(),
        line_id: input.line_id.trim(),
        enabled: input.enabled,
        mode: input.mode,
        listen_address: input.listen_address.trim(),
        listen_port: input.listen_port,
        auth_enabled: input.auth_enabled,
        username: input.auth_enabled ? input.username.trim() : '',
        has_password: input.auth_enabled,
        revision: 1,
        applied_revision: 1,
        apply_state: 'applied',
        created_at: '2026-07-23T12:05:00Z',
        updated_at: '2026-07-23T12:05:00Z'
      }
      proxies.push(proxy)
      return { proxy: clone(proxy), applied: true, status: 'applied' }
    },

    async updateProxy(id: string, input: UpdateProxyInput): Promise<ProxyMutation> {
      const index = proxies.findIndex(proxy => proxy.id === id)
      const existing = proxies[index]
      if (index < 0 || !existing) {
        throw new ApiError('代理不存在', 404, 'not_found')
      }
      if (input.revision !== existing.revision) {
        throw new ApiError('代理已被其他操作修改', 409, 'revision_conflict', 'revision')
      }
      validateFixtureProxy(input, existing)
      if (input.auth_enabled) {
        if (input.password) proxyPasswords.add(id)
      } else {
        proxyPasswords.delete(id)
      }
      const proxy: ProxyInstance = {
        ...existing,
        name: input.name.trim(),
        line_id: input.line_id.trim(),
        enabled: input.enabled,
        mode: input.mode,
        listen_address: input.listen_address.trim(),
        listen_port: input.listen_port,
        auth_enabled: input.auth_enabled,
        username: input.auth_enabled ? input.username.trim() : '',
        has_password: input.auth_enabled && proxyPasswords.has(id),
        revision: existing.revision + 1,
        applied_revision: existing.revision + 1,
        apply_state: 'applied',
        updated_at: '2026-07-23T12:06:00Z'
      }
      proxies[index] = proxy
      return { proxy: clone(proxy), applied: true, status: 'applied' }
    },

    async deleteProxy(id: string, revision: number): Promise<ProxyDeleteResult> {
      const index = proxies.findIndex(proxy => proxy.id === id)
      const existing = proxies[index]
      if (index < 0 || !existing) {
        throw new ApiError('代理不存在', 404, 'not_found')
      }
      if (revision !== existing.revision) {
        throw new ApiError('代理已被其他操作修改', 409, 'revision_conflict', 'revision')
      }
      proxies.splice(index, 1)
      proxyPasswords.delete(id)
      return { id, applied: true, status: 'applied' }
    },

    async getSIMStatus(lineID: string): Promise<SIMStatus> {
      const line = lines.find(item => fixtureLineKey(item) === lineID)
      if (!line) throw new ApiError('线路不存在', 404, 'not_found')
      const isESIM = lineID === 'line-fixture-travel'
      return {
        line_id: lineID,
        present: Boolean(line.iccid),
        active: Boolean(line.iccid),
        identifier: line.iccid,
        imsi: line.imsi,
        sim_type: isESIM ? 'esim' : 'physical',
        esim_status: isESIM ? 'with_profiles' : 'unknown',
        ...(isESIM ? { eid: '****5678' } : {}),
        sim_slots: isESIM
          ? [
              {
                index: 1,
                present: false,
                current: false,
                sim_type: 'unknown',
                esim_status: 'unknown'
              },
              {
                index: 2,
                present: true,
                current: true,
                sim_type: 'esim',
                esim_status: 'with_profiles',
                eid: '****5678'
              }
            ]
          : [
              {
                index: 1,
                present: true,
                current: true,
                sim_type: 'physical',
                esim_status: 'unknown'
              }
            ],
        sim_slots_known: true,
        primary_sim_slot: isESIM ? 2 : 1,
        primary_sim_slot_known: true,
        current_sim_slot: isESIM ? 2 : 1,
        current_sim_slot_known: true,
        profile_management: {
          supported: false,
          reason: '当前仅提供只读 eSIM 状态'
        },
        home_operator_code: line.home_operator_code,
        home_operator_name: line.home_operator_name,
        home_country_iso: line.home_country_iso,
        serving_operator_code: line.serving_operator_code,
        serving_operator_name: line.serving_operator_name,
        serving_country_iso: line.serving_country_iso,
        registration_state_known: line.registration_state_known,
        registration_state_code: line.registration_state_code,
        registration_state: line.registration_state,
        roaming: line.roaming,
        operator_identifier: line.home_operator_code,
        operator_name: line.operator,
        unlock_required: 'none',
        unlock_required_code: 1,
        unlock_retries: { 'sim-pin': 3, 'sim-puk': 10 },
        observed_at: '2026-07-23T12:00:00Z'
      }
    },

    async commandSIM(lineID: string, _input: SIMCommandInput): Promise<CommandReceipt> {
      if (!lines.some(item => fixtureLineKey(item) === lineID)) {
        throw new ApiError('线路不存在', 404, 'not_found')
      }
      return { request_id: `fixture-sim-${Date.now()}`, resource_id: lineID }
    },

    async listConnectionProfiles(lineID: string): Promise<ConnectionProfile[]> {
      return clone(connectionProfiles.get(lineID) || [])
    },

    async saveConnectionProfile(
      lineID: string,
      input: SaveConnectionProfileInput
    ): Promise<ConnectionProfile> {
      const profiles = connectionProfiles.get(lineID)
      if (!profiles) throw new ApiError('线路不存在', 404, 'not_found')
      const profileID =
        input.profile_id ?? Math.max(0, ...profiles.map(profile => profile.profile_id)) + 1
      const profile: ConnectionProfile = {
        profile_id: profileID,
        profile_name: input.profile_name || '',
        apn: input.apn || '',
        ip_family: input.ip_family || 'ipv4v6',
        ip_type: input.ip_family === 'ipv4' ? 1 : input.ip_family === 'ipv6' ? 2 : 3,
        apn_type: input.apn_type || 0,
        allowed_auth: input.allowed_auth || 0,
        user: input.user,
        access_type_preference: input.access_type_preference || 0,
        roaming_allowance: input.roaming_allowance || 0,
        profile_source: 0
      }
      connectionProfiles.set(
        lineID,
        profiles.filter(item => item.profile_id !== profileID).concat(profile)
      )
      return clone(profile)
    },

    async deleteConnectionProfile(
      lineID: string,
      input: DeleteConnectionProfileInput
    ): Promise<CommandReceipt> {
      const profiles = connectionProfiles.get(lineID)
      if (!profiles) throw new ApiError('线路不存在', 404, 'not_found')
      connectionProfiles.set(
        lineID,
        profiles.filter(
          profile =>
            (input.profile_id === undefined || profile.profile_id !== input.profile_id) &&
            (!input.profile_name || profile.profile_name !== input.profile_name)
        )
      )
      return { request_id: `fixture-profile-${Date.now()}`, resource_id: lineID }
    },

    async getUSSDStatus(lineID: string): Promise<USSDStatus> {
      if (!lines.some(item => fixtureLineKey(item) === lineID)) {
        throw new ApiError('线路不存在', 404, 'not_found')
      }
      return {
        line_id: lineID,
        state: 'idle',
        state_code: 1,
        observed_at: '2026-07-23T12:00:00Z'
      }
    },

    async commandUSSD(lineID: string, input: USSDCommandInput): Promise<USSDResponse> {
      if (!lines.some(item => fixtureLineKey(item) === lineID)) {
        throw new ApiError('线路不存在', 404, 'not_found')
      }
      return input.action === 'cancel' ? {} : { response: 'Fixture network response' }
    },

    async getDiagnostics(): Promise<DiagnosticsSnapshot> {
      return {
        status: 'ok',
        observed_at: '2026-07-23T12:00:00Z',
        database: { available: true },
        host_agent: {
          connected: true,
          provider: 'modemmanager',
          agent_version: 'fixture-agent',
          runtime_version: 'go1.26.3',
          boot_epoch: 'fixture-boot',
          revision: 'fixture-revision-8',
          observed_at: '2026-07-23T12:00:00Z',
          capabilities: {
            discovery: true,
            snapshot: true,
            device_configuration: true,
            network: true,
            proxy: true,
            dial: true,
            answer_call: true,
            reject_call: true,
            hangup_call: true,
            send_dtmf: true,
            send_message: true,
            sim_management: true,
            connection_profiles: true,
            ussd: true,
            media: false
          }
        },
        call_runtime: { available: true },
        lines: clone(
          lines.map((line, index) => ({
            ...line,
            endpoint_id: `line_fixture_endpoint_${index + 1}`,
            access_technologies: index === 0 ? 1 << 14 : 1 << 5,
            signal_snr: index === 0 ? 8.5 : undefined
          }))
        ),
        active_calls:
          activeCall && activeCall.phase !== 'ended' && activeCall.phase !== 'failed'
            ? [
                {
                  id: activeCall.id,
                  line_id: activeCall.line_id,
                  direction: activeCall.direction,
                  phase: activeCall.phase,
                  bearer: activeCall.bearer || '',
                  media_available: activeCall.media_available,
                  ...(activeCall.phase === 'active'
                    ? {
                        audio_encoding: 'pcm',
                        audio_resolution: 's16le',
                        audio_rate: 8000
                      }
                    : {})
                }
              ]
            : []
      }
    },

    async listDiagnosticLogs(query: DiagnosticLogQuery = {}): Promise<DiagnosticLogPage> {
      return filteredDiagnosticLogs(query)
    },

    subscribeDiagnosticLogs(
      _query: DiagnosticLogQuery,
      _handlers: DiagnosticLogStreamHandlers
    ): () => void {
      return () => undefined
    },

    async downloadDiagnosticLogs(query: DiagnosticLogQuery = {}): Promise<Blob> {
      const page = filteredDiagnosticLogs({ ...query, limit: 2000 })
      const content = page.entries.map(entry => JSON.stringify(entry)).join('\n')
      return new Blob([content ? `${content}\n` : ''], { type: 'application/x-ndjson' })
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

    async updateLineSettings(input: UpdateLineSettingsInput): Promise<LineSettings> {
      if (input.expected_revision !== lineSettings.revision) {
        throw new ApiError('默认线路已被其他会话修改', 409, 'conflict')
      }
      if (!lines.some(line => fixtureLineKey(line) === input.default_line_id)) {
        throw new ApiError('线路不存在', 400, 'invalid_line')
      }
      lineSettings = {
        default_line_id: input.default_line_id,
        revision: lineSettings.revision + 1
      }
      return clone(lineSettings)
    },

    async getSystemSettings(): Promise<SystemSettings> {
      return clone(systemSettings)
    },

    async updateSystemSettings(
      input: UpdateSystemSettingsInput
    ): Promise<SystemSettings> {
      if (input.expected_revision !== systemSettings.revision) {
        throw new ApiError('System settings changed in another session', 409, 'conflict')
      }
      systemSettings = {
        language: input.language,
        revision: systemSettings.revision + 1
      }
      return clone(systemSettings)
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
      if (input.operation === 'set_delivery_reports_enabled') {
        const policy = messagePolicyByLine.get(lineID)
        if (!policy) throw new ApiError('线路不存在', 404, 'not_found')
        if (input.expected_message_policy_revision !== policy.revision) {
          throw new ApiError('短信送达回执设置已被其他会话修改', 409, 'conflict')
        }
        policy.delivery_reports_enabled = input.delivery_reports_enabled
        if (input.delivery_reports_enabled) {
          policy.delivery_reports_support = 'unknown'
        }
        policy.revision += 1
        return { messaging: configurationForLine(lineID).messaging }
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
          if (!input.radio_enabled) {
            hardware.network_enabled = false
            hardware.data_connections = []
          }
          break
        case 'connect_data':
          if (
            !hardware.radio.enabled_known ||
            !hardware.radio.enabled ||
            !hardware.flight_mode_known ||
            hardware.flight_mode
          ) {
            throw new ApiError(
              '关闭飞行模式后才能开启移动数据',
              412,
              'failed_precondition'
            )
          }
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
            policy: input.volte_policy,
            configuration_mode:
              input.volte_policy === 'enabled' ? 'forced_enabled' : 'forced_disabled',
            restart_required: true
          }
          break
        case 'reprobe_voice':
          hardware.voice_verification = hardware.capabilities.voice.supported
            ? {
                usb_configuration: 'enabled',
                media_routing: 'supported'
              }
            : {
                usb_configuration: 'disabled',
                media_routing: 'disabled'
              }
          break
        case 'restart_modem':
          hardware.volte = {
            ...hardware.volte,
            restart_required: false,
            modem_capability_enabled: hardware.volte.policy === 'enabled'
          }
          break
        case 'reset_usb':
          if (!hardware.capabilities.usb_reset.writable) {
            throw new ApiError(
              hardware.capabilities.usb_reset.reason || 'USB 硬复位不可用',
              501,
              'not_supported'
            )
          }
          hardware.volte = {
            ...hardware.volte,
            restart_required: false
          }
          break
      }
      advanceHardwareRevision(lineID, hardware)
      return { hardware: clone(hardware) }
    },

    async listTelegramUnits(): Promise<TelegramUnit[]> {
      return clone(telegramUnits.map(resolvedFixtureTelegramUnit))
    },

    async createTelegramUnit(input: TelegramUnitInput): Promise<TelegramUnit> {
      if (!input.bot_token) throw new ApiError('新建 Bot 需要 token', 400)
      sequence += 1
      const assignedUser = users.find(user => user.id === input.assigned_user_id)
      const unit: TelegramUnit = {
        id: `telegram-fixture-${sequence}`,
        display_name: input.display_name,
        enabled: input.enabled,
        chat_id: input.chat_id,
        admin_id: input.admin_id,
        assigned_user_id: input.assigned_user_id,
        assigned_username: assignedUser?.username || '',
        all_assigned_lines: input.line_scopes.length === 0,
        effective_enabled: input.enabled && assignedUser?.enabled === true,
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
      const assignedUser = users.find(user => user.id === input.assigned_user_id)
      const updatedBase = { ...current }
      delete updatedBase.assigned_username
      const updated: TelegramUnit = {
        ...updatedBase,
        display_name: input.display_name,
        enabled: input.enabled,
        chat_id: input.chat_id,
        admin_id: input.admin_id,
        assigned_user_id: input.assigned_user_id,
        assigned_username: assignedUser?.username || '',
        all_assigned_lines: input.line_scopes.length === 0,
        effective_enabled: input.enabled && assignedUser?.enabled === true,
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
