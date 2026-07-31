<script setup lang="ts">
import {
  AlertCircle,
  ArrowLeft,
  AudioLines,
  Cable,
  CardSim,
  Check,
  CheckCircle2,
  Database,
  LoaderCircle,
  MessageSquareText,
  Network,
  Pencil,
  Phone,
  PhoneIncoming,
  Plane,
  Plus,
  RadioTower,
  RotateCw,
  Save,
  Send,
  ShieldAlert,
  Tag,
  Trash2,
  X
} from '@lucide/vue'
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { gateway } from '../api/client'
import { ApiError } from '../api/types'
import { useSettingsMutation } from '../composables/useSettingsMutation'
import type {
  ConnectionProfile,
  DeviceFeatureCapability,
  IncomingCallPolicy,
  IPFamily,
  LineColorPresetID,
  LineSummary,
  MobileNetwork,
  NetworkSelectionMode,
  SIMOperation,
  SIMStatus,
  USSDStatus
} from '../api/types'
import { callState } from '../state/call'
import { requestConfirmation } from '../state/confirmation'
import { showError, showSuccess } from '../state/feedback'
import {
  connectData,
  deviceConfigurationResource,
  deviceConfigurationState,
  disconnectData,
  loadDeviceConfiguration,
  reprobeVoiceCapabilities,
  restartModem,
  selectDeviceConfiguration,
  setDeliveryReportsEnabled,
  setIncomingCallPolicy,
  setRadioEnabled,
  setVoLTEPolicy
} from '../state/deviceConfiguration'
import { loadNetwork } from '../state/network'
import { sessionState } from '../state/session'
import {
  activateNetworkSelection,
  enterManualNetworkSelection,
  loadNetworkSelection,
  networkSelectionResource,
  scanMobileNetworks,
  selectManualNetwork,
  useAutomaticNetworkSelection
} from '../state/networkSelection'
import {
  bootstrapResource,
  deleteDevice as deleteDeviceRecord,
  devicesResource,
  displayModuleLines,
  lineHasCallControl,
  lineKey,
  lineLabel,
  loadBootstrap,
  loadDevices,
  renameDevice,
  updateDefaultLine,
  updateLineLabel
} from '../state/workspace'
import { operatorFacts } from '../utils/operatorNetwork'
import { LINE_TONE_PRESETS, lineTonePreset } from '../utils/lineTone'
import LineTag from './LineTag.vue'
import SensitiveValue from './SensitiveValue.vue'
import SignalBars from './SignalBars.vue'
import StatePanel from './StatePanel.vue'
import SettingsMasterDetail from './settings/SettingsMasterDetail.vue'
import SettingsSaveStatus from './settings/SettingsSaveStatus.vue'

type DeviceTab = 'overview' | 'network' | 'sim' | 'sms' | 'voice' | 'ussd'
type AsyncStatus = 'idle' | 'loading' | 'ready' | 'error'

const { t } = useI18n()
const tabs = computed<Array<{ id: DeviceTab; label: string; icon: typeof RadioTower }>>(() => [
  { id: 'overview', label: t('device.overview'), icon: RadioTower },
  { id: 'network', label: t('device.network'), icon: Network },
  { id: 'sim', label: 'SIM', icon: CardSim },
  { id: 'sms', label: t('device.smsSettings'), icon: MessageSquareText },
  { id: 'voice', label: t('device.calls'), icon: Phone },
  { id: 'ussd', label: 'USSD', icon: Send }
])

const activeTab = ref<DeviceTab>('overview')
const deviceDetailOpen = ref(false)
const apn = ref('')
const ipFamily = ref<IPFamily>('ipv4v6')
const incomingPolicyDraft = ref<IncomingCallPolicy>('follow_global')
const voltePolicyDraft = ref<'enabled' | 'disabled' | ''>('')

const moduleError = ref('')
const moduleDeletePending = ref('')
const moduleNameEditing = ref(false)
const moduleNameDraft = ref('')
const moduleNamePending = ref(false)
const moduleNameError = ref('')
const moduleNameInput = ref<HTMLInputElement | null>(null)
const lineLabelDraft = ref('')
const lineColorDraft = ref<LineColorPresetID>('teal')
const lineLabelPending = ref(false)
const lineLabelError = ref('')
const lineTonePresets = LINE_TONE_PRESETS

const simStatus = ref<SIMStatus | null>(null)
const simLoadStatus = ref<AsyncStatus>('idle')
const simError = ref('')
const simOperation = ref<SIMOperation>('send_pin')
const simPIN = ref('')
const simPUK = ref('')
const simNewPIN = ref('')
const simProtectionEnabled = ref(true)
const simPending = ref(false)
let lineServiceGeneration = 0
const profiles = ref<ConnectionProfile[]>([])
const profileLoadStatus = ref<AsyncStatus>('idle')
const profileError = ref('')
const editingProfileID = ref<number | null>(null)
const profileName = ref('')
const profileAPN = ref('')
const profileIPFamily = ref('ipv4v6')
const profileUser = ref('')
const profilePassword = ref('')
const profilePending = ref(false)

const ussdStatus = ref<USSDStatus | null>(null)
const ussdLoadStatus = ref<AsyncStatus>('idle')
const ussdError = ref('')
const ussdCommand = ref('')
const ussdResult = ref('')
const ussdPending = ref(false)

const lines = computed(() => bootstrapResource.data?.lines || [])
const moduleLines = computed(() => displayModuleLines(lines.value, devicesResource.data))
const defaultLineID = computed(
  () => bootstrapResource.data?.line_settings.default_line_id || ''
)
const selectedLineID = computed(() => deviceConfigurationState.selectedLineID)
const selectedLine = computed(() =>
  lines.value.find(line => line.id === selectedLineID.value)
)
const selectedDevice = computed(() =>
  devicesResource.data.find(device => device.imei === selectedLine.value?.device_imei)
)
const simOperatorFacts = computed(() =>
  operatorFacts(simStatus.value, '—', key => t(key))
)
const selectedModuleName = computed(() => {
  const line = selectedLine.value
  const device = selectedDevice.value
  return (
    device?.name.trim() ||
    line?.device_name.trim() ||
    device?.model.trim() ||
    line?.model?.trim() ||
    selectedLineID.value
  )
})
const selectedStoredModuleName = computed(
  () => selectedDevice.value?.name.trim() || selectedLine.value?.device_name.trim() || ''
)
const moduleNameDirty = computed(
  () => moduleNameDraft.value.trim() !== selectedStoredModuleName.value
)
const selectedExplicitLineLabel = computed(() => {
  const label = selectedLine.value?.line_label.trim() || ''
  return label.toLocaleLowerCase() === selectedModuleName.value.toLocaleLowerCase() ? '' : label
})
const currentSIMIdentity = computed(() => {
  const line = selectedLine.value
  if (!line) return ''
  const label = lineLabel(line).trim()
  const number = line.phone_number.trim()
  if (label && number && label !== number) return `${label} · ${number}`
  if (label || number) return label || number
  const identifier = simStatus.value?.identifier.trim()
  return identifier ? t('device.iccidSuffix', { suffix: identifier.slice(-4) }) : ''
})
const selectedResource = computed(() =>
  selectedLineID.value ? deviceConfigurationResource(selectedLineID.value) : null
)
const activeDeviceMutation = ref('')
const deviceMutation = useSettingsMutation({
  errorMessage: cause =>
    cause instanceof Error
      ? cause.message
      : selectedResource.value?.error || t('runtime.requestFailed')
})
const selectedNetworkSelection = computed(() =>
  selectedLineID.value ? networkSelectionResource(selectedLineID.value) : null
)
const configuration = computed(() => selectedResource.value?.data || null)
const hardware = computed(() => configuration.value?.hardware)
const selectedOperatorFacts = computed(() => {
  const line = selectedLine.value
  const flightMode =
    hardware.value?.flight_mode_known === true && hardware.value.flight_mode
  return operatorFacts(
    line && flightMode
      ? {
          ...line,
          state: 'disabled',
          registration_state_known: false,
          roaming: false
        }
      : line,
    '—',
    key => t(key)
  )
})
const apnPlaceholder = computed(() => automaticAPNLabel(hardware.value?.automatic_apn))
const incomingCalls = computed(() => configuration.value?.incoming_calls)
const messaging = computed(() => configuration.value?.messaging)
const savingOperation = computed(() => selectedResource.value?.savingOperation || '')
const hardwareBusy = computed(() => savingOperation.value !== '')

async function runDeviceMutation(
  key: string,
  operation: () => Promise<boolean>
): Promise<boolean> {
  activeDeviceMutation.value = key
  const result = await deviceMutation.run(async () => {
    const saved = await operation()
    if (!saved) {
      throw new Error(selectedResource.value?.error || t('runtime.requestFailed'))
    }
    return true
  })
  return result.ok
}
const connectedDataConnection = computed(() =>
  hardware.value?.data_connections.find(connection => connection.connected)
)
const dataConnectionStatusLabel = computed(() => {
  if (connectedDataConnection.value) return t('device.dataConnected')
  return hardware.value?.network_enabled
    ? t('device.dataConnecting')
    : t('device.dataDisconnected')
})
const dataConnectionFacts = computed(() => {
  const connection = connectedDataConnection.value
  if (!connection) return []

  const facts: Array<{ label: string; value: string }> = []
  const add = (label: string, value: string | number | undefined) => {
    if (value === undefined || value === '') return
    facts.push({ label, value: String(value) })
  }
  const addIPConfiguration = (
    family: 'IPv4' | 'IPv6',
    configuration: typeof connection.ipv4
  ) => {
    if (configuration.address) {
      add(t('device.ipAddress', { family }), configuration.address)
      add(t('device.ipPrefix', { family }), `/${configuration.prefix}`)
    }
    add(t('device.ipGateway', { family }), configuration.gateway)
    if (configuration.dns.length) {
      add(`${family} DNS`, configuration.dns.join(t('common.listSeparator')))
    }
    if (configuration.mtu > 0) add(`${family} MTU`, configuration.mtu)
  }

  add(t('traffic.interface'), connection.interface)
  add('APN', connection.apn)
  add(t('device.ipMode'), ipFamilyLabel(connection.ip_family))
  addIPConfiguration('IPv4', connection.ipv4)
  addIPConfiguration('IPv6', connection.ipv6)
  return facts
})
const selectedLineFallback = computed(() => {
  const line = selectedLine.value
  if (line) return lineLabel({ ...line, line_label: '' })
  const index = lines.value.findIndex(line => lineKey(line) === selectedLineID.value)
  return t('device.lineNumber', { number: index >= 0 ? index + 1 : 1 })
})
const selectedLineColor = computed<LineColorPresetID>(() => {
  const line = selectedLine.value
  return line ? lineTonePreset(line).id : 'teal'
})
const lineIdentityDirty = computed(
  () =>
    lineLabelDraft.value.trim() !== (selectedLine.value?.line_label || '') ||
    lineColorDraft.value !== selectedLineColor.value
)
const voiceAvailable = computed(() => lineHasCallControl(selectedLine.value))
const incomingCallControlUnavailable = computed(() => {
  const capabilities = selectedLine.value?.capabilities
  return (
    capabilities?.dial === false &&
    capabilities.answer === false &&
    capabilities.reject === false &&
    capabilities.hangup === false
  )
})
const rejectCallUnavailable = computed(
  () =>
    !incomingCallControlUnavailable.value &&
    selectedLine.value?.capabilities?.reject === false
)
const voiceMediaAvailable = computed(
  () => selectedLine.value?.capabilities?.media === true
)
const voiceCallControlWarning = computed(() => {
  const status = hardware.value?.voice_verification?.usb_configuration
  return status === 'read_failed' || status === 'invalid_response'
})
const voiceCallControlDetail = computed(() => {
  const status = hardware.value?.voice_verification?.usb_configuration
  switch (status) {
    case 'enabled':
      return voiceAvailable.value
        ? t('device.usbConfigurationConfirmed')
        : t('device.unavailable')
    case 'disabled':
      return t('device.usbCallControlDisabled')
    case 'read_failed':
      return voiceAvailable.value
        ? t('device.availableUsbReadFailed')
        : t('device.usbConfigurationReadFailed')
    case 'invalid_response':
      return voiceAvailable.value
        ? t('device.availableUsbInvalid')
        : t('device.usbConfigurationInvalid')
    default:
      return voiceAvailable.value ? t('device.available') : t('device.unavailable')
  }
})
const voiceMediaDetail = computed(() => {
  switch (hardware.value?.voice_verification?.media_routing) {
    case 'enabled':
      return t('device.mediaRoutingEnabled')
    case 'disabled':
      return t('device.mediaRoutingDisabled')
    case 'read_failed':
      return t('device.mediaRoutingReadFailed')
    case 'invalid_response':
      return t('device.mediaRoutingInvalid')
    case 'rejected':
      return t('device.mediaRoutingRejected')
    case 'inactive':
      return t('device.mediaRoutingInactive')
    case 'probe_pending':
      return t('device.mediaRoutingProbePending')
    case 'supported':
      return t('device.mediaRoutingSupported')
    default:
      return voiceMediaAvailable.value ? t('device.available') : t('device.unavailable')
  }
})
const selectedLineCall = computed(() => {
  const line = selectedLine.value
  if (!line) return null
  const key = lineKey(line)
  return (
    callState.sessions.find(
      session =>
        session.line_id === key &&
        session.phase !== 'ended' &&
        session.phase !== 'failed'
    ) || null
  )
})
const selectedCallBearer = computed(() => {
  switch (selectedLineCall.value?.bearer?.trim().toLowerCase()) {
    case 'volte':
      return 'VoLTE'
    case 'vowifi':
      return 'VoWiFi'
    case 'gsm':
    case 'cs':
    case 'circuit-switched':
      return 'GSM / CS'
    default:
      return ''
  }
})
const selectedLineVoLTEEnabled = computed(() => {
  const volte = hardware.value?.volte
  return volte?.policy_known === true && volte.policy === 'enabled'
})
const selectedLineVoLTEAvailable = computed(
  () =>
    selectedLineVoLTEEnabled.value &&
    hardware.value?.volte.modem_capability_enabled === true
)
const selectedVoiceModeTitle = computed(() =>
  selectedCallBearer.value ? t('device.currentCallBearer') : t('device.voiceMode')
)
const selectedVoiceModeLabel = computed(() => {
  if (selectedCallBearer.value) return selectedCallBearer.value
  return selectedLineVoLTEEnabled.value ? t('device.voltePreferred') : 'GSM'
})

function moduleDisplayName(line: LineSummary): string {
  const device = deviceFor(line)
  return (
    device?.name.trim() ||
    line.device_name.trim() ||
    device?.model.trim() ||
    line.model?.trim() ||
    line.phone_number.trim() ||
    lineKey(line)
  )
}

function moduleIsPresent(line: LineSummary): boolean {
  return deviceFor(line)?.present !== false && !line.module_only
}

function moduleIsDeletable(line: LineSummary): boolean {
  return Boolean(
    sessionState.role === 'admin' &&
      line.module_only &&
      deviceFor(line)?.present === false
  )
}
const flightModeWritable = computed(
  () =>
    hardware.value?.flight_mode_known === true &&
    hardware.value.capabilities.flight_mode.writable &&
    hardware.value.capabilities.radio.writable
)
const dataConnectionWritable = computed(() => {
  const current = hardware.value
  return Boolean(
    current?.capabilities.data_connection.writable &&
      current.radio.enabled_known &&
      current.radio.enabled &&
      current.flight_mode_known &&
      !current.flight_mode
  )
})
const dataConnectionDetail = computed(() => {
  const current = hardware.value
  if (!current) return ''
  const capability = current.capabilities.data_connection
  if (!capability.writable) return readOnlyReason(capability) || t('device.notWritable')
  if (!current.radio.enabled_known || !current.flight_mode_known) {
    return t('device.radioStateUnavailableForData')
  }
  if (current.flight_mode) {
    return t('runtime.turnOffFlightModeForData')
  }
  if (!current.radio.enabled) {
    return t('runtime.waitForRadioRecoveryForData')
  }
  return ''
})
const volteStatusLabel = computed(() => {
  const capability = hardware.value?.capabilities.volte
  const volte = hardware.value?.volte
  if (!capability || !volte) return t('lines.unknownState')
  if (!capability.supported) return t('device.unsupported')
  if (!capability.implemented) return t('device.notImplemented')
  if (!volte.policy_known) return t('lines.unknownState')
  if (volte.restart_required) return t('device.savedRestartRequired')
  return volte.policy === 'enabled' ? t('device.enabled') : t('device.disabled')
})
const volteCarrierConfiguration = computed(() => {
  const provisioning = hardware.value?.volte.provisioning
  if (
    !provisioning?.carrier_configuration_reported ||
    !provisioning.carrier_configuration
  ) {
    return t('device.notReported')
  }
  return provisioning.carrier_configuration_revision_reported &&
    provisioning.carrier_configuration_revision
    ? `${provisioning.carrier_configuration} · ${provisioning.carrier_configuration_revision}`
    : provisioning.carrier_configuration
})
const volteIMSProfile = computed(() => {
  const provisioning = hardware.value?.volte.provisioning
  if (!provisioning?.ims_profile_reported) return t('device.notReported')
  return provisioning.ims_profile_present
    ? t('device.detected')
    : t('device.notDetected')
})
const volteStatusDetail = computed(() => {
  const capability = hardware.value?.capabilities.volte
  const volte = hardware.value?.volte
  if (!capability || !volte) return t('device.capabilityUnknown')
  if (!capability.supported) return capability.reason || t('device.unsupported')
  if (!capability.implemented) return capability.reason || t('device.notImplemented')
  if (!capability.writable) return capability.reason || t('device.readOnly')
  if (!volte.policy_known) return t('device.unreadableStatus')
  return ''
})

function automaticAPNLabel(value?: string): string {
  const resolvedAPN = value?.trim()
  return resolvedAPN
    ? t('device.automaticAPN', { apn: resolvedAPN })
    : t('device.automatic')
}

function ipFamilyLabel(value: string): string {
  switch (value.trim().toLowerCase()) {
    case 'ipv4':
      return 'IPv4'
    case 'ipv6':
      return 'IPv6'
    case 'ipv4v6':
      return 'IPv4 + IPv6'
    default:
      return value.trim()
  }
}

function simTypeLabel(value: SIMStatus['sim_type']): string {
  switch (value) {
    case 'physical':
      return t('device.physicalSIM')
    case 'esim':
      return 'eSIM'
    default:
      return t('common.unknown')
  }
}

function esimStatusLabel(value: SIMStatus['esim_status']): string {
  switch (value) {
    case 'with_profiles':
      return t('device.existingProfiles')
    case 'no_profiles':
      return t('device.noProfiles')
    default:
      return t('common.unknown')
  }
}

const accessTechnologyLabels: Array<{ mask: number; label: string }> = [
  { mask: 1 << 15, label: '5G NR' },
  { mask: 1 << 14, label: 'LTE' },
  { mask: 1 << 16, label: 'LTE-M' },
  { mask: 1 << 17, label: 'NB-IoT' },
  { mask: 1 << 9, label: 'HSPA+' },
  { mask: 1 << 8, label: 'HSPA' },
  { mask: 1 << 7, label: 'HSUPA' },
  { mask: 1 << 6, label: 'HSDPA' },
  { mask: 1 << 5, label: 'UMTS' },
  { mask: 1 << 4, label: 'EDGE' },
  { mask: 1 << 3, label: 'GPRS' },
  { mask: 1 << 2, label: 'GSM Compact' },
  { mask: 1 << 1, label: 'GSM' },
  { mask: 1 << 13, label: 'EVDO-B' },
  { mask: 1 << 12, label: 'EVDO-A' },
  { mask: 1 << 11, label: 'EVDO-0' },
  { mask: 1 << 10, label: '1xRTT' },
  { mask: 1, label: 'POTS' }
]

function accessTechnologyLabel(value: number | null): string {
  if (value == null || value === 0) return ''
  const unsigned = value >>> 0
  const labels = accessTechnologyLabels
    .filter(item => (unsigned & item.mask) !== 0)
    .map(item => item.label)
  const knownMask = accessTechnologyLabels.reduce(
    (mask, item) => (mask | item.mask) >>> 0,
    0
  )
  const unknownMask = (unsigned & ~knownMask) >>> 0
  if (unknownMask !== 0) {
    labels.push(
      t('device.otherMask', { mask: unknownMask.toString(16).toUpperCase() })
    )
  }
  return labels.join(' / ') || `0x${unsigned.toString(16).toUpperCase()}`
}

function modemPortTypeLabel(type: string): string {
  switch (type) {
    case 'net':
      return t('device.portNetwork')
    case 'at':
      return t('device.portAT')
    case 'qcdm':
      return t('device.portQCDM')
    case 'gps':
      return 'GNSS'
    case 'qmi':
      return t('device.portQMI')
    case 'mbim':
      return t('device.portMBIM')
    case 'audio':
      return t('device.portAudio')
    case 'ignored':
      return t('device.portIgnored')
    default:
      return t('common.unknown')
  }
}

watch(
  [lines, defaultLineID],
  ([currentLines, currentDefaultLineID]) => {
    if (
      selectedLineID.value &&
      currentLines.some(line => line.id === selectedLineID.value)
    ) {
      return
    }
    const next =
      currentLines.find(line => line.id === currentDefaultLineID) ||
      currentLines.find(line => line.id)
    if (next?.id) selectLine(next, false)
  },
  { immediate: true }
)

watch(
  selectedLineID,
  () => {
    apn.value = ''
  },
  { immediate: true }
)

watch(
  [selectedLineID, () => selectedLine.value?.id || ''],
  ([lineID, availableLineID], [, previousAvailableLineID]) => {
    if (!lineID || availableLineID !== lineID) return
    void loadDeviceConfiguration(lineID, previousAvailableLineID !== lineID)
  },
  { immediate: true }
)

watch(selectedLineID, lineID => activateNetworkSelection(lineID), { immediate: true })

watch(
  () => incomingCalls.value?.revision,
  () => {
    incomingPolicyDraft.value = incomingCalls.value?.policy || 'follow_global'
  }
)

watch(
  () => hardware.value?.revision,
  () => {
    const connection =
      hardware.value?.data_connections.find(item => item.connected) ||
      hardware.value?.data_connections[0]
    ipFamily.value =
      connection?.ip_family === 'ipv4' ||
      connection?.ip_family === 'ipv6' ||
      connection?.ip_family === 'ipv4v6'
        ? connection.ip_family
        : 'ipv4v6'
  }
)

watch(
  [
    selectedLineID,
    () => hardware.value?.volte.policy_known,
    () => hardware.value?.volte.policy
  ],
  () => {
    voltePolicyDraft.value =
      hardware.value?.volte.policy_known && hardware.value.volte.policy
        ? hardware.value.volte.policy
        : ''
  },
  { immediate: true }
)

watch(
  [
    () => selectedLine.value?.iccid,
    () => selectedLine.value?.line_label,
    () => selectedLine.value?.line_color
  ],
  () => {
    lineLabelDraft.value = selectedLine.value?.line_label || ''
    lineColorDraft.value = selectedLineColor.value
    lineLabelError.value = ''
  },
  { immediate: true }
)

watch(
  [
    () => selectedLine.value?.device_imei,
    () => selectedDevice.value?.name,
    () => selectedLine.value?.device_name
  ],
  () => {
    moduleNameEditing.value = false
    moduleNameDraft.value = selectedStoredModuleName.value
    moduleNameError.value = ''
  },
  { immediate: true }
)

watch(activeTab, tab => void loadActiveLineService(tab))

function selectLine(line: LineSummary, openDetail = true): void {
  if (!line.id || line.module_only) return
  if (openDetail) deviceDetailOpen.value = true
  if (line.id === selectedLineID.value) {
    const resource = deviceConfigurationResource(line.id)
    if (resource.status !== 'ready') void loadDeviceConfiguration(line.id, true)
    return
  }
  resetLineServices()
  activateNetworkSelection(line.id)
  selectDeviceConfiguration(line.id)
  void loadActiveLineService()
}

function backToModules(): void {
  deviceDetailOpen.value = false
}

function resetLineServices(): void {
  lineServiceGeneration += 1
  simStatus.value = null
  simLoadStatus.value = 'idle'
  simError.value = ''
  profiles.value = []
  profileLoadStatus.value = 'idle'
  profileError.value = ''
  ussdStatus.value = null
  ussdLoadStatus.value = 'idle'
  ussdError.value = ''
  ussdResult.value = ''
}

async function loadActiveLineService(tab = activeTab.value): Promise<void> {
  if (tab === 'sim') {
    await loadSIM()
    return
  }
  if (tab === 'network') {
    await Promise.all([
      loadProfiles(),
      selectedLineID.value
        ? loadNetworkSelection(selectedLineID.value)
        : Promise.resolve(false)
    ])
    return
  }
  if (tab === 'ussd') await loadUSSD()
}

function isCurrentLineServiceRequest(lineID: string, generation: number): boolean {
  return selectedLineID.value === lineID && lineServiceGeneration === generation
}

function mobileNetworkName(network: MobileNetwork): string {
  return network.operator_long || network.operator_short || network.operator_code
}

function mobileNetworkTechnology(network: MobileNetwork): string {
  if (network.access_technology_names.length) {
    return network.access_technology_names.join(' / ')
  }
  return accessTechnologyLabel(network.access_technologies) || t('device.unknownTechnology')
}

function mobileNetworkStatusLabel(network: MobileNetwork): string {
  switch (network.status) {
    case 'current':
      return t('device.current')
    case 'available':
      return t('device.available')
    case 'forbidden':
      return t('device.unavailable')
    default:
      return t('lines.unknownState')
  }
}

async function changeNetworkSelectionMode(mode: NetworkSelectionMode): Promise<void> {
  const lineID = selectedLineID.value
  const target = selectedNetworkSelection.value
  if (!lineID || !target || target.policyStatus !== 'ready' || target.saving) return
  if (target.mode === mode) return
  if (mode === 'manual') {
    await enterManualNetworkSelection(lineID)
    return
  }
  await useAutomaticNetworkSelection(lineID)
}

async function refreshMobileNetworks(): Promise<void> {
  if (selectedLineID.value) await scanMobileNetworks(selectedLineID.value)
}

async function chooseMobileNetwork(network: MobileNetwork): Promise<void> {
  if (!selectedLineID.value || network.status === 'forbidden') return
  await selectManualNetwork(selectedLineID.value, network)
}

function deviceFor(line: LineSummary) {
  return devicesResource.data.find(device => device.imei === line.device_imei)
}

function readOnlyReason(capability: DeviceFeatureCapability): string {
  if (!capability.supported) return capability.reason || t('device.unsupported')
  if (!capability.implemented) return capability.reason || t('device.notImplemented')
  if (!capability.readable) return capability.reason || t('device.unreadable')
  return ''
}

function capabilityStatus(capability: DeviceFeatureCapability, id = ''): string {
  if (
    id === 'volte' &&
    hardware.value?.volte.policy_known === false &&
    capability.supported &&
    capability.implemented
  ) {
    return t('lines.unknownState')
  }
  if (capability.writable) return t('device.readWrite')
  if (capability.readable) return t('device.readOnly')
  if (capability.supported && capability.implemented) {
    return t('device.temporarilyUnavailable')
  }
  if (capability.supported) return t('device.notImplemented')
  return t('device.unsupported')
}

function policyLabel(policy: IncomingCallPolicy): string {
  if (policy === 'follow_global') return t('device.followGlobal')
  if (policy === 'receive') return t('device.receiveCalls')
  return t('device.doNotDisturb')
}

async function makeDefault(line: LineSummary): Promise<void> {
  const lineID = lineKey(line)
  if (!lineID || lineID === defaultLineID.value) return
  moduleError.value = ''
  try {
    await updateDefaultLine(lineID)
  } catch (error) {
    moduleError.value =
      error instanceof Error ? error.message : t('device.defaultLineSaveFailed')
  }
}

async function deleteHistoricalModule(line: LineSummary): Promise<void> {
  if (sessionState.role !== 'admin') return
  const device = deviceFor(line)
  if (
    !line.module_only ||
    !device ||
    device.present ||
    moduleDeletePending.value
  ) {
    return
  }
  const confirmed = await requestConfirmation({
    title: t('device.deleteModuleTitle'),
    message: t('device.deleteModuleMessage', {
      name: lineLabel(line),
      imei: device.imei
    }),
    confirmLabel: t('common.delete'),
    tone: 'danger'
  })
  if (!confirmed) return

  moduleDeletePending.value = device.imei
  moduleError.value = ''
  try {
    await deleteDeviceRecord(device.imei)
  } catch (error) {
    moduleError.value =
      error instanceof ApiError && error.code === 'device_present'
        ? t('device.deleteConnectedModuleFailed')
        : error instanceof Error
          ? error.message
          : t('device.deleteModuleFailed')
  } finally {
    moduleDeletePending.value = ''
  }
}

async function beginModuleNameEdit(): Promise<void> {
  if (sessionState.role !== 'admin') return
  if (!selectedLine.value?.device_imei || moduleNamePending.value) return
  moduleNameDraft.value = selectedStoredModuleName.value
  moduleNameError.value = ''
  moduleNameEditing.value = true
  await nextTick()
  moduleNameInput.value?.focus()
  moduleNameInput.value?.select()
}

function cancelModuleNameEdit(): void {
  if (moduleNamePending.value) return
  moduleNameDraft.value = selectedStoredModuleName.value
  moduleNameError.value = ''
  moduleNameEditing.value = false
}

async function saveModuleName(): Promise<void> {
  if (sessionState.role !== 'admin') return
  const imei = selectedLine.value?.device_imei.trim() || ''
  if (!imei || moduleNamePending.value || !moduleNameDirty.value) return
  const name = moduleNameDraft.value.trim()
  if (Array.from(name).length > 100) {
    moduleNameError.value = t('device.moduleNameTooLong')
    return
  }
  moduleNamePending.value = true
  moduleNameError.value = ''
  try {
    await renameDevice(imei, { name })
    moduleNameEditing.value = false
    showSuccess(t('common.saved'))
  } catch (error) {
    moduleNameError.value =
      error instanceof Error ? error.message : t('device.moduleNameSaveFailed')
    showError(moduleNameError.value)
  } finally {
    moduleNamePending.value = false
  }
}

async function saveLineLabel(): Promise<void> {
  const line = selectedLine.value
  const lineID = line ? lineKey(line) : ''
  if (!lineID || lineLabelPending.value || !lineIdentityDirty.value) return
  const value = lineLabelDraft.value.trim()
  if (Array.from(value).length > 16) {
    lineLabelError.value = t('device.lineLabelTooLong')
    return
  }
  lineLabelPending.value = true
  lineLabelError.value = ''
  try {
    await updateLineLabel(lineID, {
      line_label: value,
      line_color: lineColorDraft.value
    })
    showSuccess(t('common.saved'))
  } catch (error) {
    lineLabelError.value =
      error instanceof Error ? error.message : t('device.lineLabelSaveFailed')
    showError(lineLabelError.value)
  } finally {
    lineLabelPending.value = false
  }
}

function lineColorLabel(color: LineColorPresetID): string {
  return t(`device.lineColors.${color}`)
}

async function changeRadio(event: Event): Promise<void> {
  if (!selectedLineID.value) return
  const control = event.target as HTMLInputElement
  const flightModeEnabled = control.checked
  if (
    flightModeEnabled &&
    !(await requestConfirmation({
      title: t('device.enableFlightModeTitle'),
      message: t('device.enableFlightModeMessage'),
      confirmLabel: t('device.turnOn')
    }))
  ) {
    control.checked = Boolean(hardware.value?.flight_mode)
    return
  }
  const saved = await runDeviceMutation('radio', () =>
    setRadioEnabled(selectedLineID.value, !flightModeEnabled)
  )
  if (!saved) control.checked = Boolean(hardware.value?.flight_mode)
}

async function applyDataConnection(): Promise<boolean> {
  if (!selectedLineID.value || !dataConnectionWritable.value) return false
  return runDeviceMutation('data', () =>
    connectData(selectedLineID.value, apn.value, ipFamily.value)
  )
}

async function stopDataConnection(): Promise<boolean> {
  if (!selectedLineID.value) return false
  const confirmed = await requestConfirmation({
    title: t('device.disableDataTitle'),
    message: t('device.disableDataMessage'),
    confirmLabel: t('device.turnOff')
  })
  if (!confirmed) return false
  return runDeviceMutation('data', () =>
    disconnectData(selectedLineID.value)
  )
}

async function changeDataConnection(event: Event): Promise<void> {
  const control = event.target as HTMLInputElement
  const enabled = control.checked
  const saved = enabled ? await applyDataConnection() : await stopDataConnection()
  if (!saved) control.checked = Boolean(hardware.value?.network_enabled)
}

async function applyVoLTE(event: Event): Promise<void> {
  if (!selectedLineID.value) return
  const control = event.target as HTMLInputElement
  const previousPolicy =
    hardware.value?.volte.policy_known && hardware.value.volte.policy
      ? hardware.value.volte.policy
      : 'disabled'
  const nextPolicy = control.checked ? 'enabled' : 'disabled'
  voltePolicyDraft.value = nextPolicy
  const confirmed = await requestConfirmation({
    title: t('device.volteTitle', {
      action: nextPolicy === 'enabled' ? t('device.turnOn') : t('device.turnOff')
    }),
    message: t('device.restartRequiredMessage'),
    confirmLabel: t('device.saveRestartRequired')
  })
  if (!confirmed) {
    control.checked = previousPolicy === 'enabled'
    voltePolicyDraft.value = previousPolicy
    return
  }
  const saved = await runDeviceMutation('volte', () =>
    setVoLTEPolicy(selectedLineID.value, nextPolicy)
  )
  if (!saved) {
    control.checked = previousPolicy === 'enabled'
    voltePolicyDraft.value = previousPolicy
  }
}

async function reprobeVoice(): Promise<void> {
  if (!selectedLineID.value) return
  await reprobeVoiceCapabilities(selectedLineID.value)
}

async function applyModemRestart(): Promise<void> {
  if (!selectedLineID.value) return
  const confirmed = await requestConfirmation({
    title: t('device.restartTitle'),
    message: t('device.restartMessage'),
    confirmLabel: t('device.restart'),
    tone: 'danger'
  })
  if (!confirmed) return
  await restartModem(selectedLineID.value)
}

async function applyIncomingPolicy(): Promise<void> {
  if (selectedLineID.value) {
    const saved = await runDeviceMutation('incoming-policy', () =>
      setIncomingCallPolicy(selectedLineID.value, incomingPolicyDraft.value)
    )
    if (!saved) {
      incomingPolicyDraft.value = incomingCalls.value?.policy || 'follow_global'
    }
  }
}

async function applyDeliveryReports(event: Event): Promise<void> {
  if (!selectedLineID.value || !messaging.value) return
  const control = event.target as HTMLInputElement
  const previous = messaging.value.delivery_reports_enabled
  const saved = await runDeviceMutation('delivery-reports', () =>
    setDeliveryReportsEnabled(
      selectedLineID.value,
      control.checked
    )
  )
  if (!saved) control.checked = previous
}

async function loadSIM(force = false): Promise<void> {
  const lineID = selectedLineID.value
  if (!lineID || (simLoadStatus.value === 'ready' && !force)) return
  const generation = lineServiceGeneration
  simLoadStatus.value = 'loading'
  simError.value = ''
  try {
    const status = await gateway.getSIMStatus(lineID)
    if (!isCurrentLineServiceRequest(lineID, generation)) return
    simStatus.value = status
    simLoadStatus.value = 'ready'
    if (simStatus.value.unlock_required.includes('puk')) simOperation.value = 'send_puk'
    else if (simStatus.value.unlock_required.includes('pin')) simOperation.value = 'send_pin'
  } catch (error) {
    if (!isCurrentLineServiceRequest(lineID, generation)) return
    simLoadStatus.value = 'error'
    simError.value = error instanceof Error ? error.message : t('device.simLoadFailed')
  }
}

async function applySIMCommand(): Promise<void> {
  if (!selectedLineID.value || simPending.value) return
  const label: Record<SIMOperation, string> = {
    send_pin: t('device.submitPIN'),
    send_puk: t('device.submitPUK'),
    enable_pin: simProtectionEnabled.value
      ? t('device.enablePINProtection')
      : t('device.disablePINProtection'),
    change_pin: t('device.changePIN')
  }
  const confirmed = await requestConfirmation({
    title: `${label[simOperation.value]}？`,
    confirmLabel: t('device.confirm')
  })
  if (!confirmed) return
  simPending.value = true
  simError.value = ''
  try {
    await gateway.commandSIM(selectedLineID.value, {
      operation: simOperation.value,
      ...(simPIN.value ? { pin: simPIN.value } : {}),
      ...(simPUK.value ? { puk: simPUK.value } : {}),
      ...(simNewPIN.value ? { new_pin: simNewPIN.value } : {}),
      ...(simOperation.value === 'enable_pin'
        ? { enabled: simProtectionEnabled.value }
        : {})
    })
    simPIN.value = ''
    simPUK.value = ''
    simNewPIN.value = ''
    await loadSIM(true)
  } catch (error) {
    simError.value = error instanceof Error ? error.message : t('device.simOperationFailed')
  } finally {
    simPending.value = false
  }
}

async function loadProfiles(force = false): Promise<void> {
  const lineID = selectedLineID.value
  if (!lineID || (profileLoadStatus.value === 'ready' && !force)) return
  const generation = lineServiceGeneration
  profileLoadStatus.value = 'loading'
  profileError.value = ''
  try {
    const nextProfiles = await gateway.listConnectionProfiles(lineID)
    if (!isCurrentLineServiceRequest(lineID, generation)) return
    profiles.value = nextProfiles
    profileLoadStatus.value = 'ready'
  } catch (error) {
    if (!isCurrentLineServiceRequest(lineID, generation)) return
    profileLoadStatus.value = 'error'
    profileError.value =
      error instanceof Error ? error.message : t('device.profilesLoadFailed')
  }
}

function editProfile(profile?: ConnectionProfile): void {
  editingProfileID.value = profile?.profile_id ?? null
  profileName.value = profile?.profile_name || ''
  profileAPN.value = profile?.apn || ''
  profileIPFamily.value = profile?.ip_family || 'ipv4v6'
  profileUser.value = profile?.user || ''
  profilePassword.value = ''
}

async function saveProfile(): Promise<void> {
  if (!selectedLineID.value || profilePending.value) return
  if (!profileName.value.trim() && !profileAPN.value.trim()) {
    profileError.value = t('device.profileNameOrAPN')
    return
  }
  const confirmed = await requestConfirmation({
    title: t('device.saveProfileTitle'),
    confirmLabel: t('common.save')
  })
  if (!confirmed) return
  profilePending.value = true
  profileError.value = ''
  try {
    await gateway.saveConnectionProfile(selectedLineID.value, {
      ...(editingProfileID.value === null ? {} : { profile_id: editingProfileID.value }),
      profile_name: profileName.value.trim(),
      apn: profileAPN.value.trim(),
      ip_family: profileIPFamily.value,
      user: profileUser.value.trim(),
      ...(profilePassword.value ? { password: profilePassword.value } : {})
    })
    editProfile()
    await loadProfiles(true)
    showSuccess(t('common.saved'))
  } catch (error) {
    profileError.value =
      error instanceof Error ? error.message : t('device.profileSaveFailed')
    showError(profileError.value)
  } finally {
    profilePending.value = false
  }
}

async function deleteProfile(profile: ConnectionProfile): Promise<void> {
  if (!selectedLineID.value || profilePending.value) return
  const confirmed = await requestConfirmation({
    title: t('device.deleteProfileTitle'),
    message: t('device.deleteProfileMessage', {
      name: profile.profile_name || profile.profile_id
    }),
    confirmLabel: t('common.delete'),
    tone: 'danger'
  })
  if (!confirmed) return
  profilePending.value = true
  profileError.value = ''
  try {
    await gateway.deleteConnectionProfile(selectedLineID.value, {
      profile_id: profile.profile_id
    })
    if (editingProfileID.value === profile.profile_id) editProfile()
    await loadProfiles(true)
  } catch (error) {
    profileError.value =
      error instanceof Error ? error.message : t('device.profileDeleteFailed')
  } finally {
    profilePending.value = false
  }
}

async function loadUSSD(force = false): Promise<void> {
  const lineID = selectedLineID.value
  if (!lineID || (ussdLoadStatus.value === 'ready' && !force)) return
  const generation = lineServiceGeneration
  ussdLoadStatus.value = 'loading'
  ussdError.value = ''
  try {
    const status = await gateway.getUSSDStatus(lineID)
    if (!isCurrentLineServiceRequest(lineID, generation)) return
    ussdStatus.value = status
    ussdLoadStatus.value = 'ready'
  } catch (error) {
    if (!isCurrentLineServiceRequest(lineID, generation)) return
    ussdLoadStatus.value = 'error'
    ussdError.value = error instanceof Error ? error.message : t('device.ussdLoadFailed')
  }
}

async function submitUSSD(action: 'initiate' | 'respond' | 'cancel'): Promise<void> {
  if (!selectedLineID.value || ussdPending.value) return
  ussdPending.value = true
  ussdError.value = ''
  try {
    const result = await gateway.commandUSSD(selectedLineID.value, {
      action,
      ...(action === 'cancel' ? {} : { command: ussdCommand.value.trim() })
    })
    ussdResult.value = result.response || ''
    if (action !== 'respond') ussdCommand.value = ''
    await loadUSSD(true)
  } catch (error) {
    ussdError.value = error instanceof Error ? error.message : t('device.ussdRequestFailed')
  } finally {
    ussdPending.value = false
  }
}

onMounted(() => {
  void Promise.all([loadBootstrap(), loadDevices(), loadNetwork(true, true)])
})
</script>

<template>
  <SettingsMasterDetail
    class="device-configuration"
    :label="t('device.modules')"
    mobile-mode="drilldown"
    :detail-open="deviceDetailOpen"
  >
    <template #sidebar>
      <div class="device-module-list">
        <header class="module-toolbar">
          <div>
            <h3 id="device-configuration-title">{{ t('device.modules') }}</h3>
            <span>{{ t('device.moduleCount', { count: moduleLines.length }) }}</span>
          </div>
        </header>

        <StatePanel
          v-if="bootstrapResource.status === 'loading' || bootstrapResource.status === 'idle'"
          state="loading"
          :title="t('device.loadingModules')"
        />
        <StatePanel
          v-else-if="bootstrapResource.status === 'error'"
          state="error"
          :title="t('device.modulesLoadFailed')"
          :detail="bootstrapResource.error"
          retryable
          @retry="loadBootstrap(true)"
        />
        <StatePanel
          v-else-if="moduleLines.length === 0"
          state="empty"
          :title="t('device.noModules')"
        />
        <div v-else class="device-module-rows">
          <div
            v-for="line in moduleLines"
            :key="lineKey(line)"
            class="device-module-row"
            :class="{ 'is-selected': line.id === selectedLineID }"
          >
            <button
              class="device-module-row__select"
              type="button"
              :disabled="line.module_only"
              :aria-pressed="line.id === selectedLineID"
              @click="selectLine(line)"
            >
              <span class="device-module-row__icon">
                <RadioTower :size="18" />
              </span>
              <span class="device-module-row__identity">
                <strong>{{ lineLabel(line) }}</strong>
                <small>{{ moduleDisplayName(line) }}</small>
              </span>
              <span
                class="device-module-row__presence"
                :class="{ 'is-present': moduleIsPresent(line) }"
                aria-hidden="true"
              />
              <CheckCircle2
                v-if="lineKey(line) === defaultLineID"
                class="device-module-row__default"
                :size="16"
                :aria-label="t('lines.currentDefaultLine')"
              />
            </button>
            <button
              v-if="moduleIsDeletable(line)"
              class="device-module-row__delete"
              type="button"
              :disabled="moduleDeletePending === line.device_imei"
              :title="t('device.deleteModuleTitle')"
              :aria-label="t('device.deleteModuleTitle')"
              @click="deleteHistoricalModule(line)"
            >
              <LoaderCircle
                v-if="moduleDeletePending === line.device_imei"
                class="spin"
                :size="15"
              />
              <Trash2 v-else :size="15" />
            </button>
          </div>
        </div>
        <p v-if="moduleError" class="field-error device-module-list__error" role="alert">
          {{ moduleError }}
        </p>
      </div>
    </template>

    <div class="device-detail">
      <template v-if="selectedLineID">
      <header class="selected-module-context">
        <button
          class="icon-button device-detail-back"
          type="button"
          :title="t('settings.back')"
          :aria-label="t('settings.back')"
          @click="backToModules"
        >
          <ArrowLeft :size="19" />
        </button>
        <div class="selected-module-context__identity">
          <span>{{ t('device.currentModule') }}</span>
          <div class="selected-module-context__name">
            <form
              v-if="moduleNameEditing"
              class="module-name-editor"
              @submit.prevent="saveModuleName"
            >
              <input
                ref="moduleNameInput"
                v-model="moduleNameDraft"
                maxlength="100"
                autocomplete="off"
                :placeholder="selectedModuleName"
                :aria-label="t('device.moduleName')"
                :disabled="moduleNamePending"
                @keydown.escape.prevent="cancelModuleNameEdit"
              />
              <button
                class="module-name-editor__save"
                type="submit"
                :title="t('common.save')"
                :aria-label="t('common.save')"
                :disabled="moduleNamePending || !moduleNameDirty"
              >
                <LoaderCircle v-if="moduleNamePending" class="spin" :size="16" />
                <Check v-else :size="16" />
              </button>
              <button
                class="module-name-editor__cancel"
                type="button"
                :title="t('common.cancel')"
                :aria-label="t('common.cancel')"
                :disabled="moduleNamePending"
                @click="cancelModuleNameEdit"
              >
                <X :size="16" />
              </button>
            </form>
            <template v-else>
              <strong>{{ selectedModuleName }}</strong>
              <button
                v-if="sessionState.role === 'admin' && selectedLine?.device_imei"
                class="module-name-edit-button"
                type="button"
                :title="t('device.editModuleName')"
                :aria-label="t('device.editModuleName')"
                @click="beginModuleNameEdit"
              >
                <Pencil :size="15" />
              </button>
            </template>
            <LineTag
              v-if="selectedLine && selectedExplicitLineLabel"
              :line="selectedLine"
              :fallback="selectedLineFallback"
            />
            <button
              v-if="selectedLine && lineKey(selectedLine) !== defaultLineID"
              class="secondary-action selected-module-context__default"
              type="button"
              @click="makeDefault(selectedLine)"
            >
              <CheckCircle2 :size="15" />
              {{ t('lines.setDefault') }}
            </button>
          </div>
          <p v-if="moduleNameError" class="module-name-error" role="alert">
            {{ moduleNameError }}
          </p>
        </div>
      </header>

      <nav class="device-tabs" :aria-label="t('device.moduleSettings')">
        <button
          v-for="tab in tabs"
          :key="tab.id"
          type="button"
          :class="{ 'is-selected': activeTab === tab.id }"
          @click="activeTab = tab.id"
        >
          <component :is="tab.icon" :size="16" />
          {{ tab.label }}
        </button>
      </nav>

      <StatePanel
        v-if="selectedResource?.status === 'loading' || selectedResource?.status === 'idle'"
        state="loading"
        :title="t('device.loadingModule')"
      />
      <StatePanel
        v-else-if="selectedResource?.status === 'error' && !configuration"
        state="error"
        :title="t('device.moduleLoadFailed')"
        :detail="selectedResource.error"
        retryable
        @retry="loadDeviceConfiguration(selectedLineID, true)"
      />

      <div v-else-if="hardware && incomingCalls && messaging" class="device-configuration__body">
        <p v-if="selectedResource?.error" class="inline-error" role="alert">
          <AlertCircle :size="16" />
          {{ selectedResource.error }}
        </p>

        <template v-if="activeTab === 'overview'">
          <section class="configuration-section">
            <header><Tag :size="18" /><h4>{{ t('device.lineIdentity') }}</h4></header>
            <form class="line-label-form" @submit.prevent="saveLineLabel">
              <div class="line-label-form__name">
                <label>
                  <span>{{ t('device.lineLabel') }}</span>
                  <input
                    v-model="lineLabelDraft"
                    maxlength="16"
                    autocomplete="off"
                    :placeholder="t('device.suggested', { label: selectedLineFallback })"
                    :disabled="lineLabelPending || !selectedLine || !lineKey(selectedLine)"
                    aria-describedby="line-label-status"
                  />
                </label>
                <LineTag
                  v-if="selectedLine"
                  :line="{
                    ...selectedLine,
                    line_label: lineLabelDraft.trim(),
                    line_color: lineColorDraft
                  }"
                  :fallback="selectedLineFallback"
                />
              </div>
              <div class="line-label-form__controls">
                <fieldset
                  class="line-color-picker"
                  :disabled="lineLabelPending || !selectedLine || !lineKey(selectedLine)"
                >
                  <legend>{{ t('device.lineTagColor') }}</legend>
                  <div class="line-color-picker__options">
                    <label v-for="preset in lineTonePresets" :key="preset.id">
                      <input
                        v-model="lineColorDraft"
                        type="radio"
                        name="line-color"
                        :value="preset.id"
                        :aria-label="
                          t('device.selectLineColor', {
                            color: lineColorLabel(preset.id)
                          })
                        "
                      />
                      <span
                        :title="lineColorLabel(preset.id)"
                        :style="{
                          color: preset.foreground,
                          backgroundColor: preset.background,
                          borderColor: preset.border
                        }"
                      >
                        <Check v-if="lineColorDraft === preset.id" :size="14" />
                      </span>
                    </label>
                  </div>
                </fieldset>
                <div class="line-label-form__actions">
                  <button
                    class="primary-action"
                    type="submit"
                    :disabled="
                      lineLabelPending ||
                      !selectedLine ||
                      !lineKey(selectedLine) ||
                      !lineIdentityDirty
                    "
                  >
                    <LoaderCircle v-if="lineLabelPending" class="spin" :size="16" />
                    <Save v-else :size="16" />
                    {{ t('common.save') }}
                  </button>
                </div>
              </div>
            </form>
            <p
              id="line-label-status"
              class="line-label-status"
              :class="{ 'is-error': lineLabelError }"
              :role="lineLabelError ? 'alert' : 'status'"
            >
              {{
                !selectedLine || !lineKey(selectedLine)
                  ? t('device.simMissingLabel')
                  : lineLabelError
              }}
            </p>
          </section>

          <section class="configuration-section configuration-summary">
            <header>
              <h4>{{ t('device.hardwareInformation') }}</h4>
            </header>
            <dl>
              <div>
                <dt>{{ t('device.manufacturer') }}</dt>
                <dd>{{ hardware.identity.manufacturer || '—' }}</dd>
              </div>
              <div><dt>{{ t('lines.model') }}</dt><dd>{{ hardware.identity.model || '—' }}</dd></div>
              <div v-if="hardware.details.hardware_revision">
                <dt>{{ t('device.hardwareVersion') }}</dt>
                <dd>{{ hardware.details.hardware_revision }}</dd>
              </div>
              <div v-for="fact in selectedOperatorFacts" :key="fact.id">
                <dt>{{ fact.label }}</dt>
                <dd>{{ fact.value }}</dd>
              </div>
              <div>
                <dt>{{ t('lines.signal') }}</dt>
                <dd class="configuration-summary__signal">
                  <SignalBars
                    :value="selectedLine?.signal_quality"
                    :flight-mode="hardware.flight_mode_known && hardware.flight_mode"
                  />
                  <span>
                    {{
                      hardware.flight_mode_known && hardware.flight_mode
                        ? t('device.flightMode')
                        : selectedLine?.signal_quality == null
                          ? '—'
                          : `${selectedLine.signal_quality}%`
                    }}
                  </span>
                </dd>
              </div>
              <div v-if="hardware.details.access_technologies != null">
                <dt>{{ t('device.accessTechnology') }}</dt>
                <dd>{{ accessTechnologyLabel(hardware.details.access_technologies) }}</dd>
              </div>
              <div>
                <dt>IMEI</dt>
                <dd>
                  <SensitiveValue
                    :value="hardware.identity.equipment_identifier"
                    label="IMEI"
                  />
                </dd>
              </div>
              <div>
                <dt>{{ t('lines.firmware') }}</dt>
                <dd>{{ hardware.identity.firmware || '—' }}</dd>
              </div>
              <div>
                <dt>ICCID</dt>
                <dd><SensitiveValue :value="selectedLine?.iccid" label="ICCID" /></dd>
              </div>
              <div>
                <dt>{{ t('device.primaryPort') }}</dt>
                <dd>{{ hardware.details.primary_port || selectedDevice?.port || '—' }}</dd>
              </div>
              <div v-if="selectedDevice?.signal_dbm != null">
                <dt>RSSI</dt>
                <dd>{{ selectedDevice.signal_dbm }} dBm</dd>
              </div>
              <div v-if="selectedDevice?.signal_rsrp != null">
                <dt>RSRP</dt>
                <dd>{{ selectedDevice.signal_rsrp }} dBm</dd>
              </div>
              <div v-if="selectedDevice?.signal_rsrq != null">
                <dt>RSRQ</dt>
                <dd>{{ selectedDevice.signal_rsrq }} dB</dd>
              </div>
              <div v-if="hardware.details.snr != null">
                <dt>SNR</dt>
                <dd>{{ hardware.details.snr }} dB</dd>
              </div>
            </dl>
            <details v-if="hardware.details.ports.length" class="hardware-ports">
              <summary>
                <span><Cable :size="16" />{{ t('device.portDetails') }}</span>
                <small>{{ t('device.moduleCount', { count: hardware.details.ports.length }) }}</small>
              </summary>
              <ul>
                <li v-for="port in hardware.details.ports" :key="`${port.name}:${port.type_code}`">
                  <code>{{ port.name }}</code>
                  <span>{{ modemPortTypeLabel(port.type) }}</span>
                </li>
              </ul>
            </details>
          </section>
        </template>

        <template v-else-if="activeTab === 'network'">
          <section class="configuration-section">
            <header><Plane :size="18" /><h4>{{ t('device.radio') }}</h4></header>
            <label class="configuration-toggle">
              <span>
                <strong>{{ t('device.flightMode') }}</strong>
                <small v-if="!hardware.flight_mode_known">{{ t('lines.unknownState') }}</small>
                <small v-else-if="!flightModeWritable">
                  {{ readOnlyReason(hardware.capabilities.flight_mode) || t('device.notWritable') }}
                </small>
              </span>
              <span class="configuration-toggle__control">
                <SettingsSaveStatus
                  v-if="activeDeviceMutation === 'radio'"
                  :status="deviceMutation.status.value"
                  :error="deviceMutation.error.value"
                  compact
                />
                <input
                  type="checkbox"
                  role="switch"
                  :checked="hardware.flight_mode"
                  :disabled="hardwareBusy || !flightModeWritable"
                  @change="changeRadio"
                />
              </span>
            </label>
          </section>

          <section class="configuration-section">
            <header><Network :size="18" /><h4>{{ t('device.mobileNetwork') }}</h4></header>
            <label class="configuration-toggle data-toggle">
              <span>
                <strong>{{ t('device.mobileData') }}</strong>
                <small v-if="dataConnectionDetail">
                  {{ dataConnectionDetail }}
                </small>
              </span>
              <span class="configuration-toggle__control">
                <SettingsSaveStatus
                  v-if="activeDeviceMutation === 'data'"
                  :status="deviceMutation.status.value"
                  :error="deviceMutation.error.value"
                  compact
                />
                <input
                  type="checkbox"
                  role="switch"
                  :checked="hardware.network_enabled"
                  :disabled="hardwareBusy || !dataConnectionWritable"
                  @change="changeDataConnection"
                />
              </span>
            </label>
            <div class="data-primary-settings">
              <label class="data-apn-field">
                <span>APN</span>
                <input
                  v-model.trim="apn"
                  :placeholder="apnPlaceholder"
                  :disabled="
                    hardwareBusy ||
                    hardware.network_enabled ||
                    !dataConnectionWritable
                  "
                />
              </label>
              <fieldset
                class="ip-mode-field"
                :disabled="
                  hardwareBusy ||
                  hardware.network_enabled ||
                  !dataConnectionWritable
                "
              >
                <legend>{{ t('device.ipMode') }}</legend>
                <div class="ip-mode-options">
                  <label>
                    <input v-model="ipFamily" type="radio" value="ipv4" />
                    <span>IPv4</span>
                  </label>
                  <label>
                    <input v-model="ipFamily" type="radio" value="ipv6" />
                    <span>IPv6</span>
                  </label>
                  <label>
                    <input v-model="ipFamily" type="radio" value="ipv4v6" />
                    <span>IPv4 + IPv6</span>
                  </label>
                </div>
              </fieldset>
            </div>
            <div
              class="data-connection-status"
              :class="{ 'is-connected': Boolean(connectedDataConnection) }"
              role="status"
            >
              <span class="data-connection-status__dot" aria-hidden="true" />
              <span>
                <strong>{{ dataConnectionStatusLabel }}</strong>
              </span>
            </div>
            <dl v-if="dataConnectionFacts.length" class="data-connection-facts">
              <div v-for="fact in dataConnectionFacts" :key="fact.label">
                <dt>{{ fact.label }}</dt>
                <dd>{{ fact.value }}</dd>
              </div>
            </dl>

            <div class="network-selection">
              <header>
                <strong>{{ t('device.networkSelection') }}</strong>
                <small
                  v-if="
                    selectedNetworkSelection?.policy?.mode === 'manual' &&
                    selectedNetworkSelection.policy.operator_code
                  "
                >
                  {{
                    t('device.currentTarget', {
                      operator: selectedNetworkSelection.policy.operator_code
                    })
                  }}
                </small>
              </header>

              <StatePanel
                v-if="selectedNetworkSelection?.policyStatus === 'loading'"
                state="loading"
                :title="t('device.loadingNetworkSettings')"
              />
              <div
                v-else-if="
                  selectedNetworkSelection?.policyStatus === 'error' ||
                  selectedNetworkSelection?.policyStatus === 'forbidden'
                "
                class="network-selection__load-error"
              >
                <p>{{ selectedNetworkSelection.policyError }}</p>
                <button
                  class="secondary-action"
                  type="button"
                  @click="loadNetworkSelection(selectedLineID, true)"
                >
                  {{ t('common.retry') }}
                </button>
              </div>
              <template v-else-if="selectedNetworkSelection?.policyStatus === 'ready'">
                <fieldset
                  class="network-selection-mode"
                  :data-selection="selectedNetworkSelection.mode"
                  :disabled="selectedNetworkSelection.saving"
                >
                  <legend class="sr-only">{{ t('device.networkSelectionMode') }}</legend>
                  <div class="network-selection-mode__options">
                    <span class="network-selection-mode__slider" aria-hidden="true" />
                    <label>
                      <input
                        type="radio"
                        value="auto"
                        :checked="selectedNetworkSelection.mode === 'auto'"
                        @change="changeNetworkSelectionMode('auto')"
                      />
                      <span>{{ t('device.automatic') }}</span>
                    </label>
                    <label>
                      <input
                        type="radio"
                        value="manual"
                        :checked="selectedNetworkSelection.mode === 'manual'"
                        @change="changeNetworkSelectionMode('manual')"
                      />
                      <span>{{ t('device.manual') }}</span>
                    </label>
                  </div>
                  <LoaderCircle
                    v-if="selectedNetworkSelection.saving"
                    class="spin network-selection-mode__pending"
                    :size="16"
                  />
                </fieldset>

                <p
                  v-if="selectedNetworkSelection.policyError"
                  class="inline-error network-selection__policy-error"
                  role="status"
                >
                  {{ selectedNetworkSelection.policyError }}
                </p>

                <div
                  v-if="selectedNetworkSelection.mode === 'manual'"
                  class="manual-network-selection"
                >
                  <header>
                    <strong>{{ t('device.availableNetworks') }}</strong>
                    <button
                      class="secondary-action"
                      type="button"
                      :disabled="
                        selectedNetworkSelection.scanStatus === 'loading' ||
                        selectedNetworkSelection.saving
                      "
                      @click="refreshMobileNetworks"
                    >
                      <RotateCw
                        :class="{ spin: selectedNetworkSelection.scanStatus === 'loading' }"
                        :size="15"
                      />
                      {{ t('device.searchAgain') }}
                    </button>
                  </header>

                  <div
                    v-if="selectedNetworkSelection.scanStatus === 'loading'"
                    class="network-scan-state"
                    role="status"
                  >
                    <LoaderCircle class="spin" :size="17" />
                    {{ t('device.searchingNetworks') }}
                  </div>
                  <div
                    v-else-if="selectedNetworkSelection.scanStatus === 'error'"
                    class="network-scan-state is-error"
                    role="status"
                  >
                    <AlertCircle :size="17" />
                    {{ selectedNetworkSelection.scanError }}
                  </div>
                  <p
                    v-else-if="selectedNetworkSelection.scanStatus === 'idle'"
                    class="network-scan-state"
                  >
                    {{ t('device.searchToSelect') }}
                  </p>
                  <p
                    v-else-if="!selectedNetworkSelection.scan?.networks.length"
                    class="network-scan-state"
                  >
                    {{ t('device.noAvailableNetworks') }}
                  </p>
                  <div v-else class="mobile-network-list">
                    <button
                      v-for="network in selectedNetworkSelection.scan.networks"
                      :key="`${network.operator_code}-${network.access_technologies}`"
                      type="button"
                      :class="{
                        'is-selected':
                          selectedNetworkSelection.policy?.mode === 'manual' &&
                          selectedNetworkSelection.policy.operator_code ===
                            network.operator_code,
                        'is-forbidden': network.status === 'forbidden'
                      }"
                      :disabled="
                        network.status === 'forbidden' ||
                        selectedNetworkSelection.saving
                      "
                      @click="chooseMobileNetwork(network)"
                    >
                      <span class="mobile-network-list__identity">
                        <strong>{{ mobileNetworkName(network) }}</strong>
                        <small>
                          {{ network.operator_code }} · {{ mobileNetworkTechnology(network) }}
                        </small>
                      </span>
                      <span class="mobile-network-list__status">
                        <LoaderCircle
                          v-if="
                            selectedNetworkSelection.selectingOperatorCode ===
                            network.operator_code
                          "
                          class="spin"
                          :size="16"
                        />
                        <CheckCircle2
                          v-else-if="
                            selectedNetworkSelection.policy?.mode === 'manual' &&
                            selectedNetworkSelection.policy.operator_code ===
                              network.operator_code
                          "
                          :size="16"
                        />
                        <span>{{ mobileNetworkStatusLabel(network) }}</span>
                      </span>
                    </button>
                  </div>
                </div>
              </template>
            </div>
          </section>

          <section class="configuration-section">
            <details class="advanced-profiles">
              <summary>
                <span>
                  <Database :size="18" /><strong>{{ t('device.advancedProfiles') }}</strong>
                </span>
                <small v-if="profiles.length">
                  {{ t('device.moduleCount', { count: profiles.length }) }}
                </small>
              </summary>
              <div class="advanced-profiles__body">
                <div class="advanced-profiles__actions">
                  <button class="secondary-action" type="button" @click="editProfile()">
                    <Plus :size="15" />
                    {{ t('common.add') }}
                  </button>
                </div>
                <StatePanel
                  v-if="profileLoadStatus === 'loading'"
                  state="loading"
                  :title="t('device.loadingProfiles')"
                />
                <p v-else-if="profileError" class="inline-error">{{ profileError }}</p>
                <div v-else class="profile-list">
                  <div
                    v-for="profile in profiles"
                    :key="profile.profile_id"
                    :class="{ 'is-selected': editingProfileID === profile.profile_id }"
                  >
                    <button type="button" @click="editProfile(profile)">
                      <span>
                        <strong>{{ profile.profile_name || `Profile ${profile.profile_id}` }}</strong>
                        <small>{{ profile.apn || t('device.noAPN') }}</small>
                      </span>
                      <span>{{ profile.ip_family || profile.ip_type }}</span>
                    </button>
                    <button
                      class="icon-button"
                      type="button"
                      :title="t('device.deleteProfile')"
                      @click.stop="deleteProfile(profile)"
                    >
                      <Trash2 :size="16" />
                    </button>
                  </div>
                </div>
                <form class="profile-form" @submit.prevent="saveProfile">
                  <label><span>{{ t('device.name') }}</span><input v-model.trim="profileName" /></label>
                  <label><span>APN</span><input v-model.trim="profileAPN" /></label>
                  <label>
                    <span>IP</span>
                    <select v-model="profileIPFamily">
                      <option value="ipv4">IPv4</option>
                      <option value="ipv6">IPv6</option>
                      <option value="ipv4v6">IPv4 + IPv6</option>
                    </select>
                  </label>
                  <label>
                    <span>{{ t('common.username') }}</span>
                    <input v-model.trim="profileUser" autocomplete="username" />
                  </label>
                  <label>
                    <span>{{ t('common.password') }}</span>
                    <input
                      v-model="profilePassword"
                      type="password"
                      autocomplete="new-password"
                    />
                  </label>
                  <button class="primary-action" type="submit" :disabled="profilePending">
                    <LoaderCircle v-if="profilePending" class="spin" :size="16" />
                    <Save v-else :size="16" />
                    {{ t('common.save') }}
                  </button>
                </form>
              </div>
            </details>
          </section>
        </template>

        <template v-else-if="activeTab === 'sim'">
          <section class="configuration-section">
            <header><CardSim :size="18" /><h4>SIM</h4></header>
            <StatePanel
              v-if="simLoadStatus === 'loading'"
              state="loading"
              :title="t('device.loadingSIM')"
            />
            <p v-else-if="simError" class="inline-error">{{ simError }}</p>
            <template v-else-if="simStatus">
              <dl class="configuration-facts">
                <div>
                  <dt>ICCID</dt>
                  <dd><SensitiveValue :value="simStatus.identifier" label="ICCID" /></dd>
                </div>
                <div>
                  <dt>IMSI</dt>
                  <dd><SensitiveValue :value="simStatus.imsi" label="IMSI" /></dd>
                </div>
                <div v-if="simStatus.sim_type !== 'unknown'">
                  <dt>{{ t('device.simType') }}</dt>
                  <dd>{{ simTypeLabel(simStatus.sim_type) }}</dd>
                </div>
                <div v-for="fact in simOperatorFacts" :key="fact.id">
                  <dt>{{ fact.label }}</dt>
                  <dd>{{ fact.value }}</dd>
                </div>
                <div>
                  <dt>{{ t('device.locked') }}</dt>
                  <dd>{{ simStatus.unlock_required || 'none' }}</dd>
                </div>
                <template v-if="simStatus.sim_slots_known">
                  <div>
                    <dt>{{ t('device.currentSlot') }}</dt>
                    <dd>{{ simStatus.current_sim_slot_known ? simStatus.current_sim_slot : '—' }}</dd>
                  </div>
                  <div>
                    <dt>{{ t('device.primarySlot') }}</dt>
                    <dd>{{ simStatus.primary_sim_slot_known ? simStatus.primary_sim_slot : '—' }}</dd>
                  </div>
                </template>
                <template v-if="simStatus.sim_type === 'esim'">
                  <div>
                    <dt>{{ t('device.esimStatus') }}</dt>
                    <dd>{{ esimStatusLabel(simStatus.esim_status) }}</dd>
                  </div>
                  <div><dt>EID</dt><dd>{{ simStatus.eid || '—' }}</dd></div>
                  <div>
                    <dt>{{ t('device.profileManagement') }}</dt>
                    <dd>
                      {{
                        simStatus.profile_management.supported
                          ? t('device.available')
                          : t('device.notSupportedYet')
                      }}
                    </dd>
                  </div>
                </template>
              </dl>
              <div
                v-if="simStatus.sim_slots.length > 0"
                class="sim-slot-list"
                :aria-label="t('device.simSlots')"
              >
                <div
                  v-for="slot in simStatus.sim_slots"
                  :key="slot.index"
                  class="sim-slot"
                  :class="{ 'is-current': slot.current }"
                >
                  <span class="sim-slot__name">
                    {{
                      simStatus.sim_slots_known
                        ? t('device.slot', { number: slot.index })
                        : t('device.currentSIM')
                    }}
                  </span>
                  <strong class="sim-slot__state">
                    {{ slot.present ? t('device.cardInserted') : t('device.noCard') }}
                  </strong>
                  <span
                    v-if="slot.present && slot.sim_type !== 'unknown'"
                    class="sim-slot__type"
                  >
                    {{ simTypeLabel(slot.sim_type) }}
                  </span>
                  <span v-if="slot.current" class="sim-slot__badge">{{ t('device.inUse') }}</span>
                  <span v-if="slot.current && currentSIMIdentity" class="sim-slot__identity">
                    {{ currentSIMIdentity }}
                  </span>
                </div>
              </div>
              <div class="retry-row">
                <span v-for="(count, name) in simStatus.unlock_retries" :key="name">
                  {{ name }} {{ count }}
                </span>
              </div>
            </template>
          </section>

          <section class="configuration-section">
            <header><ShieldAlert :size="18" /><h4>PIN</h4></header>
            <form class="sim-form" @submit.prevent="applySIMCommand">
              <label>
                <span>{{ t('device.operation') }}</span>
                <select v-model="simOperation">
                  <option value="send_pin">{{ t('device.unlockPIN') }}</option>
                  <option value="send_puk">{{ t('device.unlockPUK') }}</option>
                  <option value="enable_pin">{{ t('device.pinProtection') }}</option>
                  <option value="change_pin">{{ t('device.changePIN') }}</option>
                </select>
              </label>
              <label v-if="simOperation !== 'send_puk'">
                <span>{{ simOperation === 'change_pin' ? t('device.currentPIN') : 'PIN' }}</span>
                <input v-model="simPIN" type="password" inputmode="numeric" maxlength="8" autocomplete="off" />
              </label>
              <label v-if="simOperation === 'send_puk'">
                <span>PUK</span>
                <input v-model="simPUK" type="password" inputmode="numeric" maxlength="8" autocomplete="off" />
              </label>
              <label v-if="simOperation === 'send_puk' || simOperation === 'change_pin'">
                <span>{{ t('device.newPIN') }}</span>
                <input v-model="simNewPIN" type="password" inputmode="numeric" maxlength="8" autocomplete="off" />
              </label>
              <label v-if="simOperation === 'enable_pin'" class="inline-check">
                <input v-model="simProtectionEnabled" type="checkbox" />
                <span>{{ t('device.enableProtection') }}</span>
              </label>
              <button class="primary-action" type="submit" :disabled="simPending">
                <LoaderCircle v-if="simPending" class="spin" :size="16" />
                {{ t('device.apply') }}
              </button>
            </form>
          </section>
        </template>

        <template v-else-if="activeTab === 'sms'">
          <section class="configuration-section">
            <header>
              <MessageSquareText :size="18" /><h4>{{ t('device.smsSettings') }}</h4>
            </header>
            <label class="configuration-toggle">
              <span>
                <strong>{{ t('device.requestDeliveryReports') }}</strong>
                <small
                  v-if="messaging.delivery_reports_support === 'unsupported'"
                  class="configuration-toggle__unsupported"
                >
                  {{ t('device.deliveryReportsUnsupported') }}
                </small>
                <small v-else>{{ t('device.deliveryReportsDescription') }}</small>
              </span>
              <span class="configuration-toggle__control">
                <SettingsSaveStatus
                  v-if="activeDeviceMutation === 'delivery-reports'"
                  :status="deviceMutation.status.value"
                  :error="deviceMutation.error.value"
                  compact
                />
                <input
                  type="checkbox"
                  role="switch"
                  :aria-label="t('device.requestDeliveryReports')"
                  :checked="messaging.delivery_reports_enabled"
                  :disabled="Boolean(savingOperation)"
                  @change="applyDeliveryReports"
                />
              </span>
            </label>
          </section>
        </template>

        <template v-else-if="activeTab === 'voice'">
          <section class="configuration-section">
            <header>
              <Phone :size="18" />
              <h4>{{ t('device.voice') }}</h4>
              <button
                class="secondary-action voice-reprobe"
                type="button"
                :disabled="hardwareBusy"
                @click="reprobeVoice"
              >
                <LoaderCircle
                  v-if="savingOperation === 'reprobe_voice'"
                  class="spin"
                  :size="16"
                />
                <RotateCw v-else :size="16" />
                {{ t('device.reprobeVoice') }}
              </button>
            </header>
            <div class="voice-capabilities">
              <div
                class="voice-status"
                :class="{
                  'is-available': voiceAvailable,
                  'is-warning': voiceCallControlWarning
                }"
              >
                <CheckCircle2 v-if="voiceAvailable" :size="18" />
                <AlertCircle v-else :size="18" />
                <span>
                  <strong>{{ t('diagnostics.callControl') }}</strong>
                  <small>{{ voiceCallControlDetail }}</small>
                </span>
              </div>
              <div
                class="voice-status"
                :class="{ 'is-available': voiceMediaAvailable }"
              >
                <AudioLines :size="18" />
                <span>
                  <strong>{{ t('lines.voiceCalling') }}</strong>
                  <small>{{ voiceMediaDetail }}</small>
                </span>
              </div>
              <div
                class="voice-status"
                :class="{ 'is-available': selectedLineVoLTEAvailable }"
              >
                <RadioTower :size="18" />
                <span>
                  <strong>{{ selectedVoiceModeTitle }}</strong>
                  <small>{{ selectedVoiceModeLabel }}</small>
                </span>
              </div>
              <div
                class="voice-status"
                :class="{ 'is-available': hardware.capabilities.vowifi.readable }"
              >
                <Network :size="18" />
                <span>
                  <strong>VoWiFi</strong>
                  <small>{{ capabilityStatus(hardware.capabilities.vowifi, 'vowifi') }}</small>
                </span>
              </div>
            </div>
          </section>

          <section class="configuration-section">
            <header>
              <PhoneIncoming :size="18" /><h4>{{ t('device.incomingCalls') }}</h4>
            </header>
            <fieldset
              class="incoming-policy"
              :disabled="Boolean(savingOperation) || incomingCallControlUnavailable"
              :aria-describedby="
                incomingCallControlUnavailable
                  ? 'incoming-policy-call-control-unavailable'
                  : rejectCallUnavailable
                    ? 'incoming-policy-reject-unavailable'
                    : undefined
              "
            >
              <legend>{{ t('device.linePolicy') }}</legend>
              <div
                class="incoming-policy__options"
                :data-selection="incomingPolicyDraft"
              >
                <span class="incoming-policy__slider" aria-hidden="true" />
                <label>
                  <input
                    v-model="incomingPolicyDraft"
                    type="radio"
                    value="follow_global"
                    @change="applyIncomingPolicy"
                  />
                  <span>{{ t('device.followGlobal') }}</span>
                </label>
                <label>
                  <input
                    v-model="incomingPolicyDraft"
                    type="radio"
                    value="receive"
                    @change="applyIncomingPolicy"
                  />
                  <span>{{ t('device.receive') }}</span>
                </label>
                <label>
                  <input
                    v-model="incomingPolicyDraft"
                    type="radio"
                    value="do_not_disturb"
                    :disabled="rejectCallUnavailable"
                    @change="applyIncomingPolicy"
                  />
                  <span>{{ t('device.doNotDisturb') }}</span>
                </label>
              </div>
            </fieldset>
            <div v-if="!incomingCallControlUnavailable" class="incoming-policy__status">
              <span>{{ t('device.current') }}</span>
              <strong>{{ policyLabel(incomingCalls.effective_policy) }}</strong>
              <SettingsSaveStatus
                v-if="activeDeviceMutation === 'incoming-policy'"
                :status="deviceMutation.status.value"
                :error="deviceMutation.error.value"
              />
              <small v-if="incomingPolicyDraft === 'follow_global'">
                {{
                  t('device.globalPolicy', {
                    policy: incomingCalls.global_receive_calls
                      ? t('device.receiveCalls')
                      : t('device.doNotDisturb')
                  })
                }}
              </small>
            </div>
            <p
              v-if="incomingCallControlUnavailable"
              id="incoming-policy-call-control-unavailable"
              class="incoming-policy__notice"
            >
              {{ t('device.callControlUnavailable') }}
            </p>
            <p
              v-else-if="rejectCallUnavailable"
              id="incoming-policy-reject-unavailable"
              class="incoming-policy__notice"
            >
              {{ t('device.rejectUnavailable') }}
            </p>
          </section>

          <section class="configuration-section">
            <header><RadioTower :size="18" /><h4>VoLTE</h4></header>
            <label class="configuration-toggle">
              <span>
                <strong>{{ t('device.enableVolte') }}</strong>
                <small v-if="volteStatusDetail">{{ volteStatusDetail }}</small>
                <small v-else>{{ volteStatusLabel }}</small>
              </span>
              <span class="configuration-toggle__control">
                <SettingsSaveStatus
                  v-if="activeDeviceMutation === 'volte'"
                  :status="deviceMutation.status.value"
                  :error="deviceMutation.error.value"
                  compact
                />
                <input
                  type="checkbox"
                  role="switch"
                  :aria-label="t('device.enableVolte')"
                  :checked="voltePolicyDraft === 'enabled'"
                  :disabled="hardwareBusy || !hardware.capabilities.volte.writable"
                  @change="applyVoLTE"
                />
              </span>
            </label>
            <dl class="configuration-facts volte-provisioning">
              <div>
                <dt>{{ t('device.carrierConfiguration') }}</dt>
                <dd>{{ volteCarrierConfiguration }}</dd>
              </div>
              <div>
                <dt>{{ t('device.imsProfile') }}</dt>
                <dd>{{ volteIMSProfile }}</dd>
              </div>
            </dl>
            <div
              v-if="hardware.volte.restart_required"
              class="restart-required"
              role="status"
            >
              <span>
                <strong>{{ t('device.waitingRestart') }}</strong>
                <small>{{ t('device.volteWritten') }}</small>
              </span>
              <button
                class="primary-action restart-action"
                type="button"
                :disabled="hardwareBusy"
                @click="applyModemRestart"
              >
                <LoaderCircle
                  v-if="savingOperation === 'restart_modem'"
                  class="spin"
                  :size="16"
                />
                <RotateCw v-else :size="16" />
                {{ t('device.restartModem') }}
              </button>
            </div>
          </section>
        </template>

        <template v-else>
          <section class="configuration-section">
            <header><Send :size="18" /><h4>USSD</h4></header>
            <StatePanel
              v-if="ussdLoadStatus === 'loading'"
              state="loading"
              :title="t('device.loadingUSSD')"
            />
            <p v-else-if="ussdError" class="inline-error">{{ ussdError }}</p>
            <div v-else-if="ussdStatus" class="ussd-status">
              <strong>{{ ussdStatus.state }}</strong>
              <span v-if="ussdStatus.network_notification">{{ ussdStatus.network_notification }}</span>
              <span v-if="ussdStatus.network_request">{{ ussdStatus.network_request }}</span>
            </div>
            <form class="ussd-form" @submit.prevent="submitUSSD(ussdStatus?.state === 'user-response' ? 'respond' : 'initiate')">
              <input v-model.trim="ussdCommand" placeholder="*123#" autocomplete="off" />
              <button class="primary-action" type="submit" :disabled="ussdPending || !ussdCommand">
                <LoaderCircle v-if="ussdPending" class="spin" :size="16" />
                <Send v-else :size="16" />
                {{ t('device.send') }}
              </button>
              <button
                v-if="ussdStatus && ussdStatus.state !== 'idle'"
                class="secondary-action"
                type="button"
                :disabled="ussdPending"
                @click="submitUSSD('cancel')"
              >
                {{ t('device.cancelSession') }}
              </button>
            </form>
            <pre v-if="ussdResult">{{ ussdResult }}</pre>
          </section>
        </template>
      </div>
      </template>
      <StatePanel
        v-else
        state="empty"
        :title="t('device.noModules')"
      />
    </div>
  </SettingsMasterDetail>
</template>

<style scoped>
.device-configuration {
  --settings-master-sidebar: 260px;

  width: 100%;
  min-width: 0;
}

.device-module-list {
  min-height: 100%;
}

.device-detail {
  min-width: 0;
  padding: 0 24px 24px;
}

.module-toolbar,
.configuration-section > header {
  display: flex;
  align-items: center;
  gap: 9px;
}

.module-toolbar {
  min-height: 62px;
  justify-content: space-between;
  padding: 0 14px;
  border-bottom: 1px solid var(--border);
}

.module-toolbar > div {
  display: flex;
  align-items: baseline;
  gap: 8px;
}

.module-toolbar h3,
.configuration-section h4 {
  margin: 0;
  font-size: 15px;
}

.module-toolbar span {
  color: var(--muted);
  font-size: 12px;
}

.device-module-rows {
  display: grid;
}

.device-module-row {
  position: relative;
  min-width: 0;
  border-bottom: 1px solid var(--border);
}

.device-module-row.is-selected {
  background: var(--surface-selected);
  box-shadow: inset 3px 0 0 var(--accent);
}

.device-module-row__select {
  display: grid;
  width: 100%;
  min-height: 72px;
  min-width: 0;
  grid-template-columns: 34px minmax(0, 1fr) 8px auto;
  align-items: center;
  gap: 9px;
  padding: 10px 12px;
  color: var(--text);
  text-align: left;
}

.device-module-row__select:disabled {
  cursor: default;
}

.device-module-row__select:not(:disabled):hover {
  background: var(--surface-hover);
}

.device-module-row__icon {
  display: grid;
  width: 34px;
  height: 34px;
  place-items: center;
  color: var(--accent-strong);
  background: var(--accent-soft);
  border-radius: 50%;
}

.device-module-row__identity {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 3px;
}

.device-module-row__identity strong,
.device-module-row__identity small {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.device-module-row__identity strong {
  font-size: 13px;
}

.device-module-row__identity small {
  color: var(--muted);
  font-size: 10px;
}

.device-module-row__presence {
  width: 7px;
  height: 7px;
  background: var(--faint);
  border-radius: 50%;
}

.device-module-row__presence.is-present {
  background: var(--success);
}

.device-module-row__default {
  color: var(--accent-strong);
}

.device-module-row__delete {
  position: absolute;
  top: 50%;
  right: 9px;
  display: grid;
  width: 30px;
  height: 30px;
  place-items: center;
  color: var(--danger);
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 6px;
  transform: translateY(-50%);
}

.device-module-list__error {
  margin: 12px;
}

.data-apn-field,
.profile-form label,
.sim-form label,
.configuration-control-row label {
  display: grid;
  gap: 5px;
}

.data-apn-field > span,
.profile-form label > span,
.sim-form label > span,
.configuration-control-row label > span {
  color: var(--muted);
  font-size: 12px;
  font-weight: 650;
}

.data-apn-field input,
.profile-form input,
.profile-form select,
.sim-form input,
.sim-form select,
.configuration-control-row select,
.ussd-form input {
  width: 100%;
  height: 36px;
  min-width: 0;
  padding: 0 9px;
  font-size: 13px;
  color: var(--text);
  background: var(--surface);
  border: 1px solid var(--border-strong);
  border-radius: 5px;
}

.primary-action,
.secondary-action {
  display: inline-flex;
  min-height: 34px;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 0 11px;
  font-size: 13px;
  font-weight: 650;
  border-radius: 5px;
}

.primary-action {
  color: #fff;
  background: var(--accent);
}

.secondary-action {
  color: var(--text);
  background: var(--surface);
  border: 1px solid var(--border-strong);
}

.primary-action:disabled,
.secondary-action:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}

.device-tabs {
  display: flex;
  min-width: 0;
  overflow-x: auto;
  border-bottom: 1px solid var(--border);
}

.device-tabs button {
  display: inline-flex;
  min-width: 92px;
  min-height: 44px;
  align-items: center;
  justify-content: center;
  gap: 6px;
  color: var(--muted);
  font-size: 13px;
  border-bottom: 2px solid transparent;
}

.device-tabs button.is-selected {
  color: var(--accent-strong);
  background: var(--surface-selected);
  border-bottom-color: var(--accent);
}

.selected-module-context {
  min-width: 0;
  padding: 13px 2px;
  border-bottom: 1px solid var(--border);
}

.device-detail-back {
  display: none;
}

.selected-module-context__identity {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 3px;
}

.selected-module-context__identity > span {
  color: var(--muted);
  font-size: 12px;
  font-weight: 650;
}

.selected-module-context__name {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 8px;
}

.selected-module-context__name > strong {
  min-width: 0;
  overflow: hidden;
  font-size: 16px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.selected-module-context__default {
  width: auto;
  margin-left: auto;
  white-space: nowrap;
}

.module-name-edit-button,
.module-name-editor button {
  display: grid;
  width: 30px;
  height: 30px;
  flex: 0 0 30px;
  place-items: center;
  color: var(--muted);
  border: 1px solid transparent;
  border-radius: 5px;
}

.module-name-edit-button:hover,
.module-name-editor button:hover:not(:disabled) {
  color: var(--accent-strong);
  background: var(--surface-selected);
  border-color: var(--border-strong);
}

.module-name-editor button.module-name-editor__save {
  color: var(--accent-strong);
}

.module-name-editor button.module-name-editor__save:hover:not(:disabled) {
  color: var(--accent-strong);
  background: var(--accent-soft);
  border-color: var(--accent);
}

.module-name-editor button.module-name-editor__cancel {
  color: var(--danger);
}

.module-name-editor button.module-name-editor__cancel:hover:not(:disabled) {
  color: var(--danger);
  background: var(--danger-soft);
  border-color: var(--danger);
}

.module-name-editor {
  display: flex;
  min-width: 0;
  max-width: min(100%, 460px);
  align-items: center;
  gap: 5px;
}

.module-name-editor input {
  width: min(360px, 55vw);
  height: 34px;
  min-width: 160px;
  padding: 0 9px;
  font-size: 15px;
}

.module-name-error {
  margin: 2px 0 0;
  color: var(--danger);
  font-size: 12px;
}

.device-configuration__body {
  min-width: 0;
}

.configuration-section {
  padding: 18px 0;
  border-bottom: 1px solid var(--border);
}

.configuration-section > header {
  min-height: 32px;
  margin-bottom: 13px;
  color: var(--accent-strong);
}

.configuration-section > header h4 {
  color: var(--text);
}

.voice-reprobe {
  margin-left: auto;
}

.line-label-form {
  display: grid;
  width: min(100%, 720px);
  gap: 12px;
}

.line-label-form__name {
  display: flex;
  min-width: 0;
  align-items: flex-end;
  gap: 10px;
  flex-wrap: wrap;
}

.line-label-form__name > label {
  display: grid;
  width: min(100%, 240px);
  gap: 5px;
}

.line-label-form__name > label > span,
.line-color-picker legend {
  color: var(--muted);
  font-size: 12px;
  font-weight: 650;
}

.line-label-form__name > label input {
  width: 100%;
  height: 36px;
  min-width: 0;
  padding: 0 9px;
  font-size: 13px;
  color: var(--text);
  background: var(--surface);
  border: 1px solid var(--border-strong);
  border-radius: 5px;
}

.line-color-picker {
  min-width: 0;
  padding: 0;
  margin: 0;
  border: 0;
}

.line-color-picker legend {
  padding: 0;
  margin-bottom: 5px;
}

.line-color-picker__options {
  display: grid;
  grid-template-columns: repeat(8, 30px);
  gap: 7px 6px;
}

.line-color-picker__options label {
  position: relative;
  display: grid;
  place-items: center;
  cursor: pointer;
}

.line-color-picker__options input {
  position: absolute;
  width: 1px;
  height: 1px;
  opacity: 0;
}

.line-color-picker__options span {
  display: grid;
  width: 30px;
  height: 26px;
  place-items: center;
  border: 1px solid;
  border-radius: 5px;
  transition:
    box-shadow 120ms ease,
    transform 120ms ease;
}

.line-color-picker__options label:hover span {
  transform: translateY(-1px);
}

.line-color-picker__options input:checked + span {
  box-shadow:
    0 0 0 2px var(--surface),
    0 0 0 4px currentColor;
}

.line-color-picker__options input:focus-visible + span {
  outline: 2px solid var(--accent);
  outline-offset: 3px;
}

.line-color-picker:disabled label {
  cursor: not-allowed;
  opacity: 0.55;
}

.line-label-form__controls {
  display: flex;
  align-items: flex-end;
  gap: 12px;
  flex-wrap: wrap;
}

.line-label-form__actions {
  display: flex;
  margin-left: auto;
  align-items: center;
  gap: 10px;
}

.line-label-form__actions .primary-action {
  width: auto;
}

.line-label-form__name > :deep(.line-tag) {
  height: 34px;
  max-width: 160px;
  padding: 0 10px;
  font-size: 13px;
  line-height: 32px;
  border-radius: 5px;
}

.line-label-status {
  min-height: 18px;
  margin: 5px 0 0;
  color: var(--accent-strong);
  font-size: 12px;
}

.line-label-status.is-error {
  color: var(--danger);
}

.section-action {
  margin-left: auto;
}

.configuration-summary dl,
.configuration-facts {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px 20px;
  margin: 0;
}

.configuration-summary dt,
.configuration-facts dt {
  color: var(--muted);
  font-size: 12px;
  font-weight: 600;
}

.configuration-summary dd,
.configuration-facts dd {
  margin: 3px 0 0;
  overflow-wrap: anywhere;
  font-size: 13px;
}

.configuration-summary__signal {
  display: flex;
  align-items: center;
  gap: 7px;
}

.hardware-ports {
  margin-top: 14px;
  padding-top: 12px;
  border-top: 1px solid var(--border);
}

.hardware-ports > summary {
  display: flex;
  min-height: 32px;
  align-items: center;
  color: var(--text);
  cursor: pointer;
  list-style-position: inside;
}

.hardware-ports > summary > span {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

.hardware-ports > summary > span svg {
  color: var(--accent-strong);
}

.hardware-ports > summary > small {
  margin-left: auto;
  color: var(--muted);
  font-size: 12px;
}

.hardware-ports ul {
  display: grid;
  gap: 0;
  margin: 8px 0 0;
  padding: 0;
  border: 1px solid var(--border);
  border-radius: 6px;
  list-style: none;
}

.hardware-ports li {
  display: flex;
  min-width: 0;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 9px 11px;
  border-bottom: 1px solid var(--border);
}

.hardware-ports li:last-child {
  border-bottom: 0;
}

.hardware-ports code {
  overflow-wrap: anywhere;
  color: var(--text);
  font-size: 12px;
}

.hardware-ports li span {
  flex: 0 0 auto;
  color: var(--muted);
  font-size: 12px;
}

.configuration-toggle,
.voice-status,
.ussd-status {
  display: flex;
  width: 100%;
  max-width: 520px;
  min-height: 56px;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.configuration-toggle > span:first-child,
.ussd-status {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  justify-content: center;
  gap: 2px;
}

.configuration-toggle small {
  color: var(--muted);
  font-size: 12px;
}

.configuration-toggle small.configuration-toggle__unsupported {
  color: #c7352d;
  font-weight: 700;
}

.configuration-toggle__control {
  display: flex;
  align-items: center;
  gap: 7px;
}

.configuration-toggle input {
  position: relative;
  width: 42px;
  height: 24px;
  flex: 0 0 42px;
  margin: 0;
  -webkit-appearance: none;
  appearance: none;
  background: #d8dde2;
  border: 0;
  border-radius: 12px;
  cursor: pointer;
  transition: background 150ms ease;
}

.configuration-toggle input::before {
  position: absolute;
  top: 3px;
  left: 3px;
  width: 18px;
  height: 18px;
  content: "";
  background: #fff;
  border-radius: 50%;
  box-shadow: 0 1px 3px rgb(16 24 40 / 20%);
  transition: transform 150ms ease;
}

.configuration-toggle input:checked {
  background: var(--accent);
}

.configuration-toggle input:checked::before {
  transform: translateX(18px);
}

.configuration-toggle input:focus-visible {
  outline: 3px solid rgb(17 120 100 / 18%);
  outline-offset: 2px;
}

.configuration-toggle input:disabled {
  cursor: not-allowed;
  opacity: 0.58;
}

.restart-required {
  display: flex;
  width: 100%;
  max-width: 520px;
  min-height: 52px;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-top: 8px;
  padding: 8px 10px;
  color: #7a420c;
  background: #fff7e8;
  border: 1px solid #e9bd72;
  border-radius: 6px;
}

.volte-provisioning {
  width: 100%;
  max-width: 680px;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  padding-top: 12px;
  margin-top: 4px;
  border-top: 1px solid var(--border);
}

.restart-required > span {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 1px;
}

.restart-required small {
  color: #8a5a25;
  font-size: 12px;
}

.restart-action {
  flex: 0 0 auto;
  background: #a85b10;
}

.data-toggle {
  padding-bottom: 12px;
  border-bottom: 1px solid var(--border);
}

.data-primary-settings {
  display: grid;
  width: 100%;
  max-width: 860px;
  grid-template-columns: minmax(180px, 320px) minmax(280px, 420px);
  align-items: end;
  gap: 14px;
  margin-top: 14px;
}

.ip-mode-field {
  min-width: 0;
  margin: 0;
  padding: 0;
  border: 0;
}

.ip-mode-field legend {
  margin-bottom: 5px;
  color: var(--muted);
  font-size: 12px;
  font-weight: 650;
}

.ip-mode-options {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 3px;
  padding: 3px;
  background: var(--surface-subtle);
  border: 1px solid var(--border-strong);
  border-radius: 6px;
}

.ip-mode-options label {
  position: relative;
  min-width: 0;
  cursor: pointer;
}

.ip-mode-options input {
  position: absolute;
  width: 1px;
  height: 1px;
  opacity: 0;
}

.ip-mode-options span {
  display: flex;
  min-height: 30px;
  align-items: center;
  justify-content: center;
  padding: 0 8px;
  color: var(--muted);
  font-size: 12px;
  font-weight: 650;
  white-space: nowrap;
  border-radius: 4px;
}

.ip-mode-options input:checked + span {
  color: var(--text);
  background: var(--surface);
  box-shadow: 0 1px 3px rgb(16 24 40 / 12%);
}

.ip-mode-options input:focus-visible + span {
  outline: 2px solid var(--accent);
  outline-offset: 1px;
}

.ip-mode-field:disabled .ip-mode-options {
  cursor: not-allowed;
  opacity: 0.55;
}

.ip-mode-field:disabled .ip-mode-options label {
  cursor: not-allowed;
}

.data-connection-status {
  display: flex;
  width: 100%;
  max-width: 860px;
  min-height: 42px;
  align-items: center;
  gap: 9px;
  margin-top: 14px;
  padding-top: 12px;
  color: var(--muted);
  border-top: 1px solid var(--border);
}

.data-connection-status__dot {
  width: 8px;
  height: 8px;
  flex: 0 0 auto;
  background: var(--muted);
  border-radius: 50%;
}

.data-connection-status.is-connected .data-connection-status__dot {
  background: var(--accent);
}

.data-connection-status > span:last-child {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 1px;
}

.data-connection-status strong {
  color: var(--text);
  font-size: 13px;
}

.data-connection-status small {
  overflow: hidden;
  color: var(--muted);
  font-size: 12px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.data-connection-facts {
  display: grid;
  width: 100%;
  max-width: 860px;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px 20px;
  padding: 12px 0;
  margin: 0;
  border-top: 1px solid var(--border);
  border-bottom: 1px solid var(--border);
}

.data-connection-facts > div {
  min-width: 0;
}

.data-connection-facts dt {
  color: var(--muted);
  font-size: 11px;
  font-weight: 650;
}

.data-connection-facts dd {
  margin: 3px 0 0;
  overflow-wrap: anywhere;
  font-size: 12px;
}

.network-selection {
  width: 100%;
  max-width: 860px;
  margin-top: 16px;
  padding-top: 16px;
  border-top: 1px solid var(--border);
}

.network-selection > header,
.manual-network-selection > header {
  display: flex;
  min-height: 34px;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.network-selection > header small {
  color: var(--muted);
  font-size: 12px;
}

.network-selection__load-error {
  display: flex;
  max-width: 520px;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.network-selection__load-error p {
  margin: 0;
  color: var(--danger);
  font-size: 12px;
}

.network-selection-mode {
  position: relative;
  width: 100%;
  max-width: 340px;
  margin: 7px 0 0;
  padding: 0 28px 0 0;
  border: 0;
}

.network-selection-mode__options {
  position: relative;
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  padding: 3px;
  overflow: hidden;
  isolation: isolate;
  background: var(--surface-subtle);
  border: 1px solid var(--border-strong);
  border-radius: 6px;
}

.network-selection-mode__slider {
  position: absolute;
  z-index: 0;
  top: 3px;
  bottom: 3px;
  left: 3px;
  width: calc((100% - 6px) / 2);
  pointer-events: none;
  background: var(--surface);
  border-radius: 4px;
  box-shadow: 0 1px 3px rgb(16 24 40 / 12%);
  transition: transform 180ms ease;
}

.network-selection-mode[data-selection='manual'] .network-selection-mode__slider {
  transform: translateX(100%);
}

.network-selection-mode__options label {
  position: relative;
  z-index: 1;
  cursor: pointer;
}

.network-selection-mode__options input {
  position: absolute;
  width: 1px;
  height: 1px;
  opacity: 0;
}

.network-selection-mode__options label > span {
  display: flex;
  min-height: 32px;
  align-items: center;
  justify-content: center;
  color: var(--muted);
  font-size: 12px;
  font-weight: 650;
  border-radius: 4px;
}

.network-selection-mode__options input:checked + span {
  color: var(--text);
}

.network-selection-mode__options input:focus-visible + span {
  outline: 2px solid var(--accent);
  outline-offset: 1px;
}

.network-selection-mode:disabled {
  opacity: 0.6;
}

.network-selection-mode:disabled label {
  cursor: not-allowed;
}

.network-selection-mode__pending {
  position: absolute;
  top: 11px;
  right: 0;
  color: var(--muted);
}

.network-selection__policy-error {
  margin-bottom: 0;
}

.manual-network-selection {
  margin-top: 14px;
  padding-top: 12px;
  border-top: 1px solid var(--border);
}

.manual-network-selection > header .secondary-action {
  min-height: 30px;
}

.network-scan-state {
  display: flex;
  min-height: 50px;
  align-items: center;
  gap: 8px;
  margin: 5px 0 0;
  color: var(--muted);
  font-size: 12px;
}

.network-scan-state.is-error {
  color: var(--danger);
}

.mobile-network-list {
  margin-top: 6px;
  border-top: 1px solid var(--border);
}

.mobile-network-list > button {
  display: grid;
  width: 100%;
  min-height: 58px;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: center;
  gap: 16px;
  padding: 9px 8px;
  color: inherit;
  text-align: left;
  border-bottom: 1px solid var(--border);
}

.mobile-network-list > button:hover:not(:disabled),
.mobile-network-list > button:focus-visible {
  background: var(--surface-selected);
}

.mobile-network-list > button:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: -2px;
}

.mobile-network-list > button.is-selected {
  color: var(--accent-strong);
}

.mobile-network-list > button.is-forbidden {
  cursor: not-allowed;
  opacity: 0.55;
}

.mobile-network-list__identity {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 2px;
}

.mobile-network-list__identity strong {
  overflow: hidden;
  font-size: 13px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.mobile-network-list__identity small,
.mobile-network-list__status {
  color: var(--muted);
  font-size: 12px;
}

.mobile-network-list__status {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.mobile-network-list > button.is-selected .mobile-network-list__status {
  color: var(--accent-strong);
}

@media (prefers-reduced-motion: reduce) {
  .network-selection-mode__slider {
    transition: none;
  }
}

.advanced-profiles > summary {
  min-height: 32px;
  color: var(--text);
  cursor: pointer;
}

.advanced-profiles > summary::marker {
  color: var(--muted);
}

.advanced-profiles > summary > span {
  display: inline-flex;
  align-items: center;
  gap: 9px;
  margin-left: 5px;
}

.advanced-profiles > summary > span svg {
  color: var(--accent-strong);
}

.advanced-profiles > summary > small {
  float: right;
  margin-top: 3px;
  color: var(--muted);
  font-size: 12px;
}

.advanced-profiles__body {
  margin-top: 12px;
  padding-top: 12px;
  border-top: 1px solid var(--border);
}

.advanced-profiles__actions {
  display: flex;
  justify-content: flex-end;
}

.profile-list {
  display: grid;
  gap: 7px;
  margin-top: 12px;
}

.profile-list > div {
  display: grid;
  min-width: 0;
  min-height: 48px;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: center;
  gap: 10px;
  padding: 4px;
  background: var(--surface-subtle);
  border: 1px solid var(--border);
  border-radius: 5px;
}

.profile-list > div > button:first-child {
  display: grid;
  min-width: 0;
  min-height: 38px;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: center;
  gap: 10px;
  padding: 4px 6px;
  color: inherit;
  text-align: left;
  background: transparent;
}

.profile-list small,
.profile-list > div > button > span:nth-child(2) {
  color: var(--muted);
  font-size: 12px;
}

.profile-list > div.is-selected {
  border-color: var(--accent);
}

.profile-list > div > button > span:first-child {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 2px;
}

.profile-form,
.sim-form {
  display: grid;
  grid-template-columns: repeat(5, minmax(120px, 1fr)) auto;
  align-items: end;
  gap: 9px;
  margin-top: 12px;
}

.sim-form {
  grid-template-columns: repeat(4, minmax(130px, 1fr)) auto;
}

.inline-check {
  display: flex !important;
  min-height: 36px;
  align-items: center;
  gap: 7px;
}

.inline-check input {
  width: 15px;
  height: 15px;
}

.retry-row {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 12px;
}

.sim-slot-list {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 12px;
  padding-top: 12px;
  border-top: 1px solid var(--border);
}

.sim-slot {
  display: grid;
  min-width: 190px;
  grid-template-columns: minmax(0, 1fr) auto;
  grid-template-areas:
    "name badge"
    "state badge"
    "type badge"
    "identity identity";
  gap: 3px 10px;
  padding: 10px 12px;
  background: var(--surface-subtle);
  border: 1px solid var(--border);
  border-radius: 6px;
}

.sim-slot__name {
  grid-area: name;
  color: var(--muted);
  font-size: 11px;
}

.sim-slot__state {
  grid-area: state;
  font-size: 13px;
  font-weight: 650;
}

.sim-slot__type {
  grid-area: type;
  color: var(--muted);
  font-size: 11px;
}

.sim-slot__badge {
  grid-area: badge;
  align-self: center;
  color: var(--accent-strong);
  font-size: 11px;
  font-weight: 650;
}

.sim-slot__identity {
  grid-area: identity;
  min-width: 0;
  overflow: hidden;
  color: var(--muted);
  font-size: 11px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.sim-slot.is-current {
  border-color: var(--accent);
  background: var(--accent-soft);
}

.retry-row span {
  padding: 4px 7px;
  color: var(--muted);
  font-size: 12px;
  background: var(--surface-subtle);
  border-radius: 4px;
}

.configuration-control-row {
  display: grid;
  width: 100%;
  max-width: 680px;
  grid-template-columns: minmax(180px, 520px) auto;
  align-items: end;
  justify-content: start;
  gap: 9px;
}

.configuration-control-row + .configuration-facts {
  margin-top: 13px;
}

.configuration-control-status {
  display: flex;
  flex-wrap: wrap;
  gap: 5px 8px;
  margin: 9px 0 0;
  color: var(--muted);
  font-size: 12px;
}

.configuration-control-status strong {
  color: var(--text);
}

.incoming-policy {
  width: 100%;
  max-width: 520px;
  margin: 0;
  padding: 0;
  border: 0;
}

.incoming-policy legend {
  margin-bottom: 5px;
  color: var(--muted);
  font-size: 12px;
  font-weight: 650;
}

.incoming-policy__options {
  position: relative;
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 0;
  padding: 3px;
  overflow: hidden;
  isolation: isolate;
  background: var(--surface-subtle);
  border: 1px solid var(--border-strong);
  border-radius: 6px;
}

.incoming-policy__slider {
  position: absolute;
  z-index: 0;
  top: 3px;
  bottom: 3px;
  left: 3px;
  width: calc((100% - 6px) / 3);
  pointer-events: none;
  background: var(--surface);
  border-radius: 4px;
  box-shadow: 0 1px 3px rgb(16 24 40 / 12%);
  transform: translateX(0);
  transition: transform 180ms ease;
}

.incoming-policy__options[data-selection='receive'] .incoming-policy__slider {
  transform: translateX(100%);
}

.incoming-policy__options[data-selection='do_not_disturb'] .incoming-policy__slider {
  transform: translateX(200%);
}

.incoming-policy__options label {
  position: relative;
  z-index: 1;
  min-width: 0;
  cursor: pointer;
}

.incoming-policy__options input {
  position: absolute;
  width: 1px;
  height: 1px;
  opacity: 0;
}

.incoming-policy__options span {
  display: flex;
  min-height: 32px;
  align-items: center;
  justify-content: center;
  padding: 0 8px;
  color: var(--muted);
  font-size: 12px;
  font-weight: 650;
  border-radius: 4px;
}

.incoming-policy__options input:checked + span {
  color: var(--text);
}

.incoming-policy__options input:focus-visible + span {
  outline: 2px solid var(--accent);
  outline-offset: 1px;
}

.incoming-policy:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}

.incoming-policy:disabled label {
  cursor: not-allowed;
}

@media (prefers-reduced-motion: reduce) {
  .incoming-policy__slider {
    transition: none;
  }
}

.incoming-policy__status {
  display: flex;
  min-height: 28px;
  align-items: center;
  gap: 6px;
  color: var(--muted);
  font-size: 12px;
}

.incoming-policy__status strong {
  color: var(--text);
}

.incoming-policy__status small {
  margin-left: 4px;
  color: var(--muted);
}

.incoming-policy__notice {
  min-height: 28px;
  margin: 0;
  color: var(--muted);
  font-size: 12px;
  line-height: 28px;
}

.incoming-policy__options input:disabled + span {
  cursor: not-allowed;
  opacity: 0.5;
}

.voice-capabilities {
  display: grid;
  width: 100%;
  max-width: 1120px;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 190px), 1fr));
  gap: 8px;
}

.voice-status {
  justify-content: flex-start;
  padding: 0 12px;
  color: var(--danger);
  background: var(--surface-subtle);
  border: 1px solid var(--border);
  border-radius: 5px;
}

.voice-status span {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.voice-status small {
  color: var(--muted);
  font-size: 12px;
}

.voice-status.is-available {
  color: var(--accent-strong);
}

.voice-status.is-warning {
  color: #8a4b10;
}

.voice-status.is-pending {
  color: var(--muted-strong);
}

.inline-error,
.inline-warning {
  display: flex;
  align-items: flex-start;
  gap: 7px;
  margin: 10px 0;
  color: var(--danger);
  font-size: 12px;
}

.inline-warning {
  color: #8a4b10;
}

.ussd-form {
  display: flex;
  gap: 8px;
  margin-top: 12px;
}

.ussd-form input {
  flex: 1;
}

.ussd-status {
  gap: 4px;
}

.ussd-status span {
  color: var(--muted);
  font-size: 12px;
}

pre {
  margin: 12px 0 0;
  padding: 10px;
  overflow: auto;
  font-size: 12px;
  white-space: pre-wrap;
  background: var(--surface-subtle);
  border: 1px solid var(--border);
  border-radius: 5px;
}

@media (max-width: 980px) {
  .profile-form,
  .sim-form {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

}

@media (max-width: 720px) {
  .device-detail {
    padding: 0 16px 76px;
  }

  .selected-module-context {
    display: flex;
    align-items: flex-start;
    gap: 8px;
  }

  .device-detail-back {
    display: grid;
    flex: 0 0 auto;
  }

  .selected-module-context__default {
    width: auto;
  }

  .line-label-form,
  .data-primary-settings,
  .profile-form,
  .sim-form,
  .configuration-control-row,
  .configuration-summary dl,
  .configuration-facts,
  .data-connection-facts {
    grid-template-columns: 1fr;
  }

  .primary-action,
  .secondary-action {
    width: 100%;
  }

  .restart-required {
    max-width: none;
    align-items: stretch;
    flex-direction: column;
  }

  .module-toolbar > .secondary-action,
  .section-action,
  .manual-network-selection > header .secondary-action {
    width: auto;
  }

  .ussd-form {
    flex-direction: column;
  }

  .voice-capabilities {
    grid-template-columns: 1fr;
  }
}

</style>
