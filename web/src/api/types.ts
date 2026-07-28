export type CapabilityName = 'dial' | 'message'
export type CommunicationCapabilityName =
  | 'modem'
  | 'sim'
  | 'voice'
  | 'messaging'
  | 'media'
  | 'dial'
  | 'answer'
  | 'reject'
  | 'hangup'
  | 'dtmf'
  | 'message'

export type CommunicationCapabilities = Partial<Record<CommunicationCapabilityName, boolean>>

export const LINE_COLOR_PRESET_IDS = [
  'teal',
  'blue',
  'indigo',
  'violet',
  'green',
  'amber',
  'orange',
  'red'
] as const

export type LineColorPresetID = (typeof LINE_COLOR_PRESET_IDS)[number]

const LINE_COLOR_PRESET_ID_SET = new Set<string>(LINE_COLOR_PRESET_IDS)

export function isLineColorPresetID(value: unknown): value is LineColorPresetID {
  return typeof value === 'string' && LINE_COLOR_PRESET_ID_SET.has(value)
}

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
  id: string
  iccid: string
  imsi: string
  phone_number: string
  operator: string
  home_operator_code: string
  home_operator_name: string
  serving_operator_code: string
  serving_operator_name: string
  registration_state_known: boolean
  registration_state_code: number
  registration_state: string
  roaming: boolean
  emergency_only: boolean
  device_imei: string
  device_name: string
  line_label: string
  line_color?: LineColorPresetID | ''
  model?: string
  firmware?: string
  hardware_revision?: string
  primary_port?: string
  ports?: ModemPort[]
  access_technologies?: number
  state?: string
  radio_desired_enabled: boolean
  radio_desired_enabled_known: boolean
  signal_quality?: number
  signal_snr?: number
  capabilities?: CommunicationCapabilities
  module_only?: boolean
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
  line_id: string
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

export type TLSMode = 'automatic' | 'user'

export type TLSSettings = {
  mode: TLSMode
  subject: string
  issuer: string
  dns_names: string[]
  ip_addresses: string[]
  not_before: string
  not_after: string
  fingerprint_sha256: string
  expired: boolean
  renews_automatically: boolean
}

export type AboutInfo = {
  name: string
  version: string
  commit: string
  build_date: string
  repository_url: string
  license_name: string
  license_url: string
  notices_url: string
}

export type UpdateStatus =
  | 'up_to_date'
  | 'update_available'
  | 'development'
  | 'unavailable'

export type UpdateCheck = {
  status: UpdateStatus
  current_version: string
  latest_version?: string
  release_name?: string
  release_url?: string
  published_at?: string
  checked_at: string
  error_code?: string
}

export type UpdateTLSSettingsInput =
  | {
      operation: 'install_user'
      certificate_pem: string
      private_key_pem: string
    }
  | {
      operation: 'use_automatic'
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

export type RecordingStatus = 'pending' | 'recording' | 'ready' | 'failed'

export type RecordingEntry = {
  id: string
  call_id: string
  segment_index: number
  status: RecordingStatus
  recorded_at: string
  started_at?: string
  ended_at?: string
  duration_seconds: number
  size_bytes: number
  failure_code?: string
  playable: boolean
  content_type?: string
  download_url?: string
  call: CallRecord
}

export type BootstrapResponse = {
  capabilities: Capabilities
  lines: LineSummary[]
  line_settings: LineSettings
  system_settings: SystemSettings
}

export type LineSettings = {
  default_line_id: string
  revision: number
}

export type ProxyMode = 'http' | 'socks5'
export type ProxyRuntimeState = 'disabled' | 'waiting_for_bearer' | 'running' | 'error'
export type ProxyApplyState =
  | 'applied'
  | 'pending_create'
  | 'pending_update'
  | 'pending_delete'
export type ProxyApplyStatus =
  | 'pending'
  | 'applied'
  | 'agent_unavailable'
  | 'agent_rejected'
  | 'runtime_unavailable'

export type NetworkLineStatus = {
  line_id: string
  connected: boolean
  interface: string
  addresses: string[]
  dns: string[]
  rx_bytes: number
  tx_bytes: number
  error: string
}

export type NetworkProxyStatus = {
  id: string
  line_id: string
  state: ProxyRuntimeState
  running: boolean
  mode: ProxyMode
  listen_address: string
  listen_port: number
  interface: string
  runtime_epoch: string
  started_at?: string
  bytes_up: number
  bytes_down: number
  connections: number
  active_connections: number
  last_error: string
}

export type NetworkUsage = {
  scope_kind: 'line' | 'proxy'
  scope_id: string
  rx_bytes: number
  tx_bytes: number
}

export type NetworkUsageTotal = {
  rx_bytes: number
  tx_bytes: number
}

export type NetworkStatus = {
  available: boolean
  state: string
  unavailable_reason?: string
  boot_epoch: string
  observed_at?: string
  lines: NetworkLineStatus[]
  proxies: NetworkProxyStatus[]
  today_total: NetworkUsageTotal
  today_usage: NetworkUsage[]
  month_total: NetworkUsageTotal
  month_usage: NetworkUsage[]
  stale: boolean
  apply_pending: boolean
  apply_status: ProxyApplyStatus
  apply_attempts: number
  apply_exhausted: boolean
}

export type NetworkSelectionMode = 'auto' | 'manual'
export type MobileNetworkStatus = 'unknown' | 'available' | 'current' | 'forbidden'

export type NetworkSelectionPolicy = {
  line_id: string
  mode: NetworkSelectionMode
  operator_code?: string
  revision: number
  applied: boolean
  last_error?: string
  applied_at?: string
}

export type UpdateNetworkSelectionInput = {
  mode: NetworkSelectionMode
  operator_code?: string
  expected_revision: number
}

export type MobileNetwork = {
  status: MobileNetworkStatus
  operator_code: string
  operator_long: string
  operator_short: string
  access_technologies: number
  access_technology_names: string[]
}

export type MobileNetworkScan = {
  line_id: string
  observed_at: string
  networks: MobileNetwork[]
}

export type ProxyInstance = {
  id: string
  name: string
  line_id: string
  enabled: boolean
  mode: ProxyMode
  listen_address: string
  listen_port: number
  auth_enabled: boolean
  username: string
  has_password: boolean
  revision: number
  applied_revision: number
  apply_state: ProxyApplyState
  created_at: string
  updated_at: string
}

export type CreateProxyInput = {
  name: string
  line_id: string
  enabled: boolean
  mode: ProxyMode
  listen_address: string
  listen_port: number
  auth_enabled: boolean
  username: string
  password: string
}

export type UpdateProxyInput = Omit<CreateProxyInput, 'password'> & {
  revision: number
  password?: string
}

export type ProxyMutation = {
  proxy: ProxyInstance
  applied: boolean
  status: ProxyApplyStatus
}

export type ProxyDeleteResult = {
  id: string
  applied: boolean
  status: ProxyApplyStatus
}

export type UpdateLineSettingsInput = {
  default_line_id: string
  expected_revision: number
}

export type SystemLanguage = 'auto' | 'zh-CN' | 'en-US'

export type SystemSettings = {
  language: SystemLanguage
  revision: number
}

export type UpdateSystemSettingsInput = {
  language: SystemLanguage
  expected_revision: number
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
  avatar?: string
  phones: ContactPhone[]
  favorite: boolean
  notes?: string
  preferred_line_id?: string
  revision?: number
  created_at?: string
  updated_at?: string
}

export type ContactInput = {
  display_name: string
  avatar?: string
  favorite: boolean
  phones: Array<{
    id?: string
    label: string
    number: string
    primary: boolean
  }>
  notes?: string
  preferred_line_id?: string
  revision?: number
}

export type MessageThread = {
  key: string
  line_id: string
  peer: string
  contact_name?: string
  last_timestamp: string
  last_content?: string
  unread_count: number
}

export type Message = {
  id: string
  line_id: string
  peer: string
  direction: CallDirection
  content: string
  timestamp: string
  type: 1 | 2
  status: number
}

export type IncomingMessageEvent = {
  id: number
  event_key: string
  message_id: string
  thread_key: string
  line_id: string
  peer: string
  content: string
  timestamp: string
}

export type MessageEventStreamHandlers = {
  onOpen: () => void
  onReady: (newestID: number) => void
  onMessage: (event: IncomingMessageEvent) => void
  onReset: (oldestID: number, newestID: number) => void
  onError: (error?: Error) => void
}

export type RuntimeResource = 'lines' | 'network' | 'calls' | 'messages'

export type RuntimeEvent = {
  id: number
  resources: RuntimeResource[]
  observed_at: string
}

export type RuntimeEventStreamHandlers = {
  onOpen: () => void
  onReady: (newestID: number) => void
  onEvent: (event: RuntimeEvent) => void
  onReset: (oldestID: number, newestID: number) => void
  onError: (error?: Error) => void
}

export type MessageReadInput = {
  line_id: string
  peer: string
}

export type SendMessageInput = {
  thread_key?: string
  request_id?: string
  line_id: string
  to: string
  content: string
}

export type CallRecord = {
  id: string
  line_id: string
  direction: CallDirection
  remote_number: string
  display_name?: string
  contact_id?: string
  started_at: string
  ended_at?: string
  duration_seconds: number
  missed: boolean
  read: boolean
  failure_reason?: string
}

export type DeviceSIM = {
  iccid: string
  imsi: string
  phone_number: string
  operator: string
  home_operator_code: string
  home_operator_name: string
  serving_operator_code: string
  serving_operator_name: string
  registration_state_known: boolean
  registration_state_code: number
  registration_state: string
  roaming: boolean
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
  endpoint_id: string
  name: string
  model: string
  firmware: string
  port: string
  public_ip: string
  private_ip: string
  public_ipv6: string
  private_ipv6: string
  state?: string
  current_iccid: string
  sim_inserted: boolean
  signal_quality: number | null
  signal_dbm: number | null
  signal_rsrq: number | null
  signal_rsrp: number | null
  last_seen?: string
  present: boolean
  sim?: DeviceSIM
  capabilities?: CommunicationCapabilities
}

export type CreateDeviceInput = {
  imei: string
  name?: string
}

export type RenameDeviceInput = {
  name: string
}

export type UpdateLineLabelInput = {
  line_label: string
  line_color?: LineColorPresetID
}

export type LineLabelResult = {
  line_id: string
  line_label: string
  line_color: LineColorPresetID | ''
}

export type CommandReceipt = {
  request_id: string
  resource_id: string
}

export type SIMType = 'unknown' | 'physical' | 'esim'

export type ESIMStatus = 'unknown' | 'no_profiles' | 'with_profiles'

export type SIMSlot = {
  index: number
  present: boolean
  current: boolean
  sim_type: SIMType
  esim_status: ESIMStatus
  eid?: string
}

export type SIMProfileManagementCapability = {
  supported: boolean
  reason: string
}

export type SIMStatus = {
  line_id: string
  present: boolean
  active: boolean
  identifier: string
  imsi: string
  sim_type: SIMType
  esim_status: ESIMStatus
  eid?: string
  sim_slots: SIMSlot[]
  sim_slots_known: boolean
  primary_sim_slot: number
  primary_sim_slot_known: boolean
  current_sim_slot: number
  current_sim_slot_known: boolean
  profile_management: SIMProfileManagementCapability
  home_operator_code: string
  home_operator_name: string
  serving_operator_code: string
  serving_operator_name: string
  registration_state_known: boolean
  registration_state_code: number
  registration_state: string
  roaming: boolean
  operator_identifier: string
  operator_name: string
  unlock_required: string
  unlock_required_code: number
  unlock_retries: Record<string, number>
  observed_at: string
}

export type SIMOperation = 'send_pin' | 'send_puk' | 'enable_pin' | 'change_pin'

export type SIMCommandInput = {
  operation: SIMOperation
  pin?: string
  puk?: string
  new_pin?: string
  enabled?: boolean
}

export type ConnectionProfile = {
  profile_id: number
  profile_name: string
  apn: string
  ip_family: string
  ip_type: number
  apn_type: number
  allowed_auth: number
  user?: string
  access_type_preference: number
  roaming_allowance: number
  profile_source: number
}

export type SaveConnectionProfileInput = {
  profile_id?: number
  profile_name?: string
  apn?: string
  ip_family?: string
  apn_type?: number
  allowed_auth?: number
  user?: string
  password?: string
  access_type_preference?: number
  roaming_allowance?: number
}

export type DeleteConnectionProfileInput = {
  profile_id?: number
  profile_name?: string
}

export type USSDStatus = {
  line_id: string
  state: string
  state_code: number
  network_notification?: string
  network_request?: string
  observed_at: string
}

export type USSDAction = 'initiate' | 'respond' | 'cancel'

export type USSDCommandInput = {
  action: USSDAction
  command?: string
}

export type USSDResponse = {
  response?: string
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

export type ModemPort = {
  name: string
  type: string
  type_code: number
}

export type DeviceHardwareDetails = {
  hardware_revision: string
  primary_port: string
  access_technologies: number | null
  snr: number | null
  ports: ModemPort[]
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

export type VoiceRuntimeVerification = {
  usb_configuration:
    | 'enabled'
    | 'disabled'
    | 'read_failed'
    | 'invalid_response'
  media_routing:
    | 'enabled'
    | 'disabled'
    | 'read_failed'
    | 'invalid_response'
    | 'rejected'
    | 'inactive'
}

export type VoLTEConfiguration = {
  policy_known: boolean
  policy?: 'enabled' | 'disabled'
  configuration_mode?: 'automatic' | 'forced_enabled' | 'forced_disabled'
  modem_capability_known: boolean
  modem_capability_enabled: boolean
  restart_required: boolean
  profile_id?: string
  provisioning: VoLTEProvisioning
}

export type VoLTEProvisioning = {
  backend: string
  carrier_configuration?: string
  carrier_configuration_reported: boolean
  carrier_configuration_revision?: string
  carrier_configuration_revision_reported: boolean
  ims_profile_reported: boolean
  ims_profile_present: boolean
}

export type DeviceConfigurationCapabilities = {
  voice: DeviceFeatureCapability
  radio: DeviceFeatureCapability
  data_connection: DeviceFeatureCapability
  flight_mode: DeviceFeatureCapability
  vowifi: DeviceFeatureCapability
  volte: DeviceFeatureCapability
  esim: DeviceFeatureCapability
  at_terminal: DeviceFeatureCapability
  ussd: DeviceFeatureCapability
  connection_profile: DeviceFeatureCapability
  usb_reset: DeviceFeatureCapability
}

export type DeviceHardwareConfiguration = {
  line_id: string
  revision: string
  observed_at: string
  identity: DeviceIdentity
  details: DeviceHardwareDetails
  radio: RadioConfiguration
  flight_mode: boolean
  flight_mode_known: boolean
  network_enabled: boolean
  automatic_apn: string
  data_connections: DataConnection[]
  voice_verification?: VoiceRuntimeVerification
  volte: VoLTEConfiguration
  capabilities: DeviceConfigurationCapabilities
}

export type DeviceConfiguration = {
  hardware?: DeviceHardwareConfiguration
  incoming_calls?: LineIncomingCallConfiguration
}

export type DiagnosticStatus = 'ok' | 'degraded' | 'unavailable'

export type DiagnosticAvailability = {
  available: boolean
  error?: string
}

export type DiagnosticHostAgent = {
  connected: boolean
  provider: string
  agent_version: string
  runtime_version: string
  boot_epoch: string
  revision: string
  observed_at: string
  last_error?: string
  capabilities: DiagnosticAgentCapabilities
}

export type DiagnosticAgentCapabilities = {
  discovery: boolean
  snapshot: boolean
  device_configuration: boolean
  network: boolean
  proxy: boolean
  dial: boolean
  answer_call: boolean
  reject_call: boolean
  hangup_call: boolean
  send_dtmf: boolean
  send_message: boolean
  sim_management: boolean
  connection_profiles: boolean
  ussd: boolean
  media: boolean
}

export type DiagnosticActiveCall = {
  id: string
  line_id: string
  endpoint_line_id?: string
  direction: string
  phase: string
  bearer: string
  media_available: boolean
  audio_encoding?: string
  audio_resolution?: string
  audio_rate?: number
}

export type DiagnosticsSnapshot = {
  status: DiagnosticStatus
  observed_at: string
  database: DiagnosticAvailability
  host_agent: DiagnosticHostAgent
  call_runtime: DiagnosticAvailability
  lines: LineSummary[]
  active_calls: DiagnosticActiveCall[]
}

export type DiagnosticLogLevel = 'debug' | 'info' | 'warn' | 'error'

export type DiagnosticLogEntry = {
  id: number
  timestamp: string
  level: DiagnosticLogLevel
  component: string
  caller?: string
  message: string
  fields?: Record<string, unknown>
}

export type DiagnosticLogPage = {
  entries: DiagnosticLogEntry[]
  oldest_id: number
  newest_id: number
  truncated: boolean
}

export type DiagnosticLogQuery = {
  after?: number
  limit?: number
  level?: DiagnosticLogLevel
  component?: string
  search?: string
}

export type DiagnosticLogStreamHandlers = {
  onOpen: () => void
  onEntry: (entry: DiagnosticLogEntry) => void
  onReset: (oldestID: number, newestID: number) => void
  onError: (error?: Error) => void
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
  | {
      request_id: string
      operation: 'restart_modem'
      expected_device_revision: string
    }
  | {
      request_id: string
      operation: 'reset_usb'
      expected_device_revision: string
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
  setup_required: boolean
  username?: string
  csrf_token?: string
  language: SystemLanguage
}

export type SetupInput = {
  username: string
  password: string
}

export type LoginInput = {
  username: string
  password: string
}

export type ChangePasswordInput = {
  current_password: string
  new_password: string
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
