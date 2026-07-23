export type CapabilityName = 'dial' | 'message'

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
  iccid: string
  imsi: string
  phone_number: string
  operator: string
  device_imei: string
  device_alias: string
}

export type CallDirection = 'incoming' | 'outgoing'
export type CallPhase = 'dialing' | 'ringing' | 'connecting' | 'active' | 'ending' | 'ended' | 'failed'
export type CallFilter = 'all' | 'missed' | 'incoming' | 'outgoing'

export type CallSession = {
  id: string
  line_key: string
  direction: CallDirection
  remote_number: string
  display_name?: string
  phase: CallPhase
  created_at: string
  active_at?: string
  ended_at?: string
  failure_reason?: string
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
  iccid: string
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
  current_iccid: string
  sim_inserted: boolean
  signal_dbm: number | null
  sim?: DeviceSIM
}

export type ApiErrorBody = {
  code?: string
  message?: string
  detail?: string
}

export class ApiError extends Error {
  readonly status: number
  readonly code?: string

  constructor(message: string, status = 0, code?: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
  }
}

export type ResourceStatus = 'idle' | 'loading' | 'ready' | 'error'

export type Resource<T> = {
  status: ResourceStatus
  data: T
  error: string
}
