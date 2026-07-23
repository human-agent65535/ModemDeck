export type CapabilityName = 'dial' | 'message'
export type CommunicationCapabilityName =
  | 'dial'
  | 'answer'
  | 'reject'
  | 'hangup'
  | 'dtmf'
  | 'message'

export type CommunicationCapabilities = Partial<Record<CommunicationCapabilityName, boolean>>

export type Capabilities = {
  agent_connected: boolean
  dial: boolean
  message: boolean
  webrtc_audio: boolean
  device_control: boolean
  volte_control: boolean
  vowifi_control: boolean
  unavailable_reasons?: Partial<Record<CapabilityName, string>>
}

export type LineSummary = {
  id?: string
  iccid: string
  imsi: string
  phone_number: string
  operator: string
  device_imei: string
  device_alias: string
  state?: string
  capabilities?: CommunicationCapabilities
}

export type CallDirection = 'incoming' | 'outgoing'
export type CallPhase =
  | 'unknown'
  | 'dialing'
  | 'ringing'
  | 'connecting'
  | 'active'
  | 'ending'
  | 'ended'
  | 'failed'
export type CallFilter = 'all' | 'missed' | 'incoming' | 'outgoing'
export type CallAction = 'answer' | 'reject' | 'hangup'

export type CallSession = {
  id: string
  line_key: string
  direction: CallDirection
  remote_number: string
  display_name?: string
  phase: CallPhase
  media_available: boolean
  created_at: string
  active_at?: string
  ended_at?: string
  bearer?: string
  failure_reason?: string
}

export type RecordingSettings = {
  default_enabled: boolean
  revision: number
}

export type CallRecordingState = {
  call_id: string
  enabled: boolean
  active: boolean
  started_at?: string
  error?: string
}

export type CallRecording = {
  id: string
  call_id: string
  started_at: string
  ended_at?: string
  duration_seconds: number
  content_type: string
  size_bytes: number
  download_url: string
}

export type BootstrapResponse = {
  capabilities: Capabilities
  lines: LineSummary[]
}

export type ContactPhone = {
  id: string
  label: string
  number: string
  normalized_number?: string
  primary: boolean
}

export type Contact = {
  id: string
  display_name: string
  phones: ContactPhone[]
  notes?: string
  revision?: number
  created_at?: string
  updated_at?: string
}

export type ContactInput = {
  display_name: string
  phones: Array<{
    id?: string
    label: string
    number: string
    primary: boolean
  }>
  notes?: string
  revision?: number
}

export type MessageThread = {
  key: string
  imsi: string
  iccid: string
  peer: string
  contact_name?: string
  last_timestamp: string
  last_content?: string
  unread_count: number
}

export type Message = {
  id: string
  imsi: string
  iccid: string
  peer: string
  direction: CallDirection
  content: string
  timestamp: string
  type: 1 | 2
  status: number
}

export type SendMessageInput = {
  thread_key?: string
  request_id?: string
  line_id?: string
  iccid?: string
  to: string
  content: string
}

export type CallRecord = {
  id: string
  device_id: string
  direction: CallDirection
  remote_number: string
  display_name?: string
  contact_id?: string
  started_at: string
  ended_at?: string
  duration_seconds: number
  missed: boolean
  failure_reason?: string
}

export type DeviceSIM = {
  iccid: string
  imsi: string
  phone_number: string
  operator: string
  current_imei: string
  reg_status: number
  reg_status_text: string
  lac: string
  cell_id: string
  apn: string
  ims_status: number
  last_seen: string
}

export type Device = {
  imei: string
  alias: string
  model: string
  firmware: string
  state?: string
  current_iccid: string
  sim_inserted: boolean
  signal_dbm: number | null
  sim?: DeviceSIM
  capabilities?: CommunicationCapabilities
}

export type IncomingCallPolicy = 'follow_global' | 'receive' | 'do_not_disturb'
export type EffectiveIncomingCallPolicy = 'receive' | 'do_not_disturb'

export type CallPolicyEnforcement = {
  mode: string
  max_submissions_per_call: number
  new_incoming_ringing_only: boolean
  available: boolean
  config_only: boolean
  reason?: string
}

export type GlobalCallSettings = {
  receive_calls: boolean
  revision: number
}

export type IncomingCallActionResult = {
  call_id: string
  effective_policy: EffectiveIncomingCallPolicy
  status: string
  error_code?: string
  created_at: string
  updated_at: string
}

export type LineIncomingCallConfiguration = {
  policy: IncomingCallPolicy
  revision: number
  effective_policy: EffectiveIncomingCallPolicy
  global_receive_calls: boolean
  global_revision: number
  updated_at: string
  enforcement: CallPolicyEnforcement
  last_action?: IncomingCallActionResult
}

export type DeviceFeatureCapability = {
  backend: string
  supported: boolean
  implemented: boolean
  readable: boolean
  writable: boolean
  reason?: string
}

export type DeviceIdentity = {
  manufacturer: string
  model: string
  firmware: string
  equipment_identifier: string
}

export type RadioConfiguration = {
  enabled: boolean
  enabled_known: boolean
  power_state: string
  power_state_code: number
}

export type IPConfiguration = {
  method: string
  address: string
  prefix: number
  gateway: string
  dns: string[]
  mtu: number
}

export type DataConnection = {
  id: string
  connected: boolean
  apn: string
  ip_family: string
  interface: string
  ipv4: IPConfiguration
  ipv6: IPConfiguration
}

export type VoLTEConfiguration = {
  policy_known: boolean
  policy?: 'enabled' | 'disabled'
  profile_id?: string
}

export type DeviceConfigurationCapabilities = {
  radio: DeviceFeatureCapability
  data_connection: DeviceFeatureCapability
  flight_mode: DeviceFeatureCapability
  vowifi: DeviceFeatureCapability
  volte: DeviceFeatureCapability
  alias: DeviceFeatureCapability
  esim: DeviceFeatureCapability
  at_terminal: DeviceFeatureCapability
  ussd: DeviceFeatureCapability
  connection_profile: DeviceFeatureCapability
}

export type DeviceHardwareConfiguration = {
  line_id: string
  revision: string
  observed_at: string
  identity: DeviceIdentity
  radio: RadioConfiguration
  flight_mode: boolean
  flight_mode_known: boolean
  network_enabled: boolean
  data_connections: DataConnection[]
  volte: VoLTEConfiguration
  capabilities: DeviceConfigurationCapabilities
}

export type DeviceConfiguration = {
  hardware?: DeviceHardwareConfiguration
  incoming_calls?: LineIncomingCallConfiguration
}

export type IPFamily = 'auto' | 'ipv4' | 'ipv6' | 'ipv4v6'

export type UpdateGlobalCallSettingsInput = {
  receive_calls: boolean
  expected_revision: number
}

export type UpdateDeviceConfigurationInput =
  | {
      operation: 'set_incoming_call_policy'
      expected_policy_revision: number
      incoming_call_policy: IncomingCallPolicy
    }
  | {
      request_id: string
      operation: 'set_radio_enabled'
      expected_device_revision: string
      radio_enabled: boolean
    }
  | {
      request_id: string
      operation: 'connect_data'
      expected_device_revision: string
      apn: string
      ip_family: IPFamily
    }
  | {
      request_id: string
      operation: 'disconnect_data'
      expected_device_revision: string
    }
  | {
      request_id: string
      operation: 'set_volte_policy'
      expected_device_revision: string
      volte_policy: 'enabled' | 'disabled'
    }

export type TelegramUnit = {
  id: string
  display_name: string
  enabled: boolean
  chat_id: string
  admin_id: string
  line_scopes: string[]
  incoming_sms: boolean
  missed_calls: boolean
  token_configured: boolean
  bot_username?: string
  revision: number
}

export type TelegramUnitInput = {
  display_name: string
  enabled: boolean
  chat_id: string
  admin_id: string
  line_scopes: string[]
  incoming_sms: boolean
  missed_calls: boolean
  bot_token?: string
  revision?: number
}

export type ApiErrorBody = {
  code?: string
  message?: string
  detail?: string
  field?: string
}

export type SessionResponse = {
  authenticated: boolean
  username?: string
  csrf_token?: string
}

export type LoginInput = {
  username: string
  password: string
}

export class ApiError extends Error {
  readonly status: number
  readonly code?: string
  readonly field?: string

  constructor(message: string, status = 0, code?: string, field?: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
    this.field = field
  }
}

export type ResourceStatus = 'idle' | 'loading' | 'ready' | 'forbidden' | 'error'

export type Resource<T> = {
  status: ResourceStatus
  data: T
  error: string
}
