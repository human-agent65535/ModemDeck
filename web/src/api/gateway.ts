import type {
  ActiveCallSnapshot,
  AboutInfo,
  BootstrapResponse,
  CallAction,
  CallMediaICEConfiguration,
  CallLeaseStatus,
  CallFilter,
  CallBatchAction,
  CallRecordingSegment,
  CallRecordingState,
  CallRecord,
  CallSession,
  ChangePasswordInput,
  CommandReceipt,
  ConnectionProfile,
  Contact,
  ContactInput,
  ContactRevision,
  CreateMemberInput,
  CreateDeviceInput,
  CreateProxyInput,
  DeleteConnectionProfileInput,
  Device,
  DeviceConfiguration,
  DiagnosticLogPage,
  DiagnosticLogQuery,
  DiagnosticLogStreamHandlers,
  DiagnosticsSnapshot,
  GlobalCallSettings,
  LineLabelResult,
  LineSettings,
  LoginInput,
  Message,
  MessageEventStreamHandlers,
  MessageReadInput,
  MessageThreadAction,
  MessageThread,
  MobileNetworkScan,
  NetworkSelectionPolicy,
  NetworkStatus,
  Page,
  ProxyDeleteResult,
  ProxyInstance,
  ProxyMutation,
  RecordingEntry,
  RecordingBatchAction,
  RecordingIdentity,
  RecordingSettings,
  RenameDeviceInput,
  RuntimeEventStreamHandlers,
  SaveConnectionProfileInput,
  SendMessageInput,
  SessionResponse,
  SetupInput,
  SystemSettings,
  SIMCommandInput,
  SIMStatus,
  TelegramUnit,
  TelegramUnitInput,
  IOSPairingResult,
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

export type ListQuery = {
  q?: string
  cursor?: string
  limit?: number
}

export type MessageQuery = {
  line_id: string
  peer: string
  cursor?: string
  limit?: number
}

export type GatewayInteractions = Readonly<{
  contacts: boolean
  message: boolean
  dial: boolean
  telegram: boolean
}>

export interface SessionGateway {
  getSession(): Promise<SessionResponse>
  setup(input: SetupInput): Promise<SessionResponse>
  login(input: LoginInput): Promise<SessionResponse>
  changePassword(input: ChangePasswordInput): Promise<void>
  setAccountContact(contactID: string): Promise<void>
  logout(): Promise<void>
}

export interface ModemDeckGateway {
  readonly interactions?: GatewayInteractions
  getAbout(): Promise<AboutInfo>
  checkForUpdates(): Promise<UpdateCheck>
  getBootstrap(): Promise<BootstrapResponse>
  listUsers(): Promise<UserAccount[]>
  createMember(input: CreateMemberInput): Promise<UserAccount>
  updateMember(id: string, input: UpdateMemberInput): Promise<UserAccount>
  setMemberPassword(id: string, password: string): Promise<void>
  listContacts(query?: ListQuery): Promise<Page<Contact>>
  listThreads(query?: ListQuery): Promise<Page<MessageThread>>
  listMessages(query: MessageQuery): Promise<Page<Message>>
  subscribeMessageEvents(handlers: MessageEventStreamHandlers): () => void
  subscribeRuntimeEvents(handlers: RuntimeEventStreamHandlers): () => void
  markThreadRead(input: MessageReadInput): Promise<void>
  updateMessageThreads(
    action: MessageThreadAction,
    threads: MessageReadInput[]
  ): Promise<void>
  deleteThread(input: MessageReadInput): Promise<void>
  listCalls(filter?: CallFilter, query?: ListQuery): Promise<Page<CallRecord>>
  markMissedCallsRead(): Promise<void>
  markMissedCallRead(id: string): Promise<void>
  updateCalls(action: CallBatchAction, ids: string[]): Promise<void>
  deleteCall(id: string): Promise<void>
  listDevices(): Promise<Device[]>
  createDevice(input: CreateDeviceInput): Promise<Device>
  renameDevice(imei: string, input: RenameDeviceInput): Promise<Device>
  deleteDevice(imei: string): Promise<void>
  updateLineLabel(lineID: string, input: UpdateLineLabelInput): Promise<LineLabelResult>
  getNetworkStatus(): Promise<NetworkStatus>
  getNetworkSelection(lineID: string): Promise<NetworkSelectionPolicy>
  updateNetworkSelection(
    lineID: string,
    input: UpdateNetworkSelectionInput
  ): Promise<NetworkSelectionPolicy>
  scanMobileNetworks(lineID: string, signal?: AbortSignal): Promise<MobileNetworkScan>
  listProxies(): Promise<ProxyInstance[]>
  createProxy(input: CreateProxyInput): Promise<ProxyMutation>
  updateProxy(id: string, input: UpdateProxyInput): Promise<ProxyMutation>
  deleteProxy(id: string, revision: number): Promise<ProxyDeleteResult>
  getSIMStatus(lineID: string): Promise<SIMStatus>
  commandSIM(lineID: string, input: SIMCommandInput): Promise<CommandReceipt>
  listConnectionProfiles(lineID: string): Promise<ConnectionProfile[]>
  saveConnectionProfile(
    lineID: string,
    input: SaveConnectionProfileInput
  ): Promise<ConnectionProfile>
  deleteConnectionProfile(
    lineID: string,
    input: DeleteConnectionProfileInput
  ): Promise<CommandReceipt>
  getUSSDStatus(lineID: string): Promise<USSDStatus>
  commandUSSD(lineID: string, input: USSDCommandInput): Promise<USSDResponse>
  getDiagnostics(): Promise<DiagnosticsSnapshot>
  getDiagnosticDeviceConfiguration(lineID: string): Promise<DeviceConfiguration>
  resetDiagnosticUSB(
    lineID: string,
    expectedDeviceRevision: string
  ): Promise<DeviceConfiguration>
  listDiagnosticLogs(query?: DiagnosticLogQuery): Promise<DiagnosticLogPage>
  subscribeDiagnosticLogs(
    query: DiagnosticLogQuery,
    handlers: DiagnosticLogStreamHandlers
  ): () => void
  downloadDiagnosticLogs(query?: DiagnosticLogQuery): Promise<Blob>
  getGlobalCallSettings(): Promise<GlobalCallSettings>
  updateGlobalCallSettings(input: UpdateGlobalCallSettingsInput): Promise<GlobalCallSettings>
  updateLineSettings(input: UpdateLineSettingsInput): Promise<LineSettings>
  getSystemSettings(): Promise<SystemSettings>
  updateSystemSettings(input: UpdateSystemSettingsInput): Promise<SystemSettings>
  getIOSPairing(): Promise<IOSPairingResult>
  createIOSPairing(): Promise<IOSPairingResult>
  revokeIOSPairing(): Promise<void>
  getTLSSettings(): Promise<TLSSettings>
  updateTLSSettings(input: UpdateTLSSettingsInput): Promise<TLSSettings>
  getDeviceConfiguration(lineID: string): Promise<DeviceConfiguration>
  updateDeviceConfiguration(
    lineID: string,
    input: UpdateDeviceConfigurationInput
  ): Promise<DeviceConfiguration>
  createContact?(input: ContactInput): Promise<Contact>
  updateContact?(id: string, input: ContactInput): Promise<Contact>
  deleteContact?(id: string, revision?: number): Promise<void>
  deleteContacts?(contacts: ContactRevision[]): Promise<void>
  sendMessage(input: SendMessageInput): Promise<Message>
  getActiveCallSnapshot(): Promise<ActiveCallSnapshot>
  startCall(
    lineKey: string,
    number: string,
    recordingEnabled?: boolean
  ): Promise<CallSession>
  callAction(id: string, action: CallAction): Promise<void>
  sendDTMF(id: string, digit: string): Promise<void>
  renewCallLease(id: string): Promise<CallLeaseStatus>
  getCallMediaICEConfiguration(
    id: string,
    signal?: AbortSignal
  ): Promise<CallMediaICEConfiguration>
  exchangeCallMedia(
    id: string,
    ownerToken: string,
    offerSDP: string,
    signal?: AbortSignal
  ): Promise<string>
  releaseCallMedia(id: string, ownerToken: string): Promise<void>
  listRecordings(query?: ListQuery): Promise<Page<RecordingEntry>>
  getRecordingSettings(): Promise<RecordingSettings>
  updateRecordingSettings(settings: RecordingSettings): Promise<RecordingSettings>
  setCallRecording(id: string, enabled: boolean): Promise<CallRecordingState>
  listCallRecordings(id: string): Promise<CallRecordingSegment[]>
  deleteRecording(callID: string, recordingID: string): Promise<void>
  updateRecordings(
    action: RecordingBatchAction,
    recordings: RecordingIdentity[]
  ): Promise<void>
  listTelegramUnits(): Promise<TelegramUnit[]>
  createTelegramUnit(input: TelegramUnitInput): Promise<TelegramUnit>
  updateTelegramUnit(id: string, input: TelegramUnitInput): Promise<TelegramUnit>
  deleteTelegramUnit(id: string, revision: number): Promise<void>
}

export type ConfiguredModemDeckGateway = ModemDeckGateway & {
  readonly interactions: GatewayInteractions
} & SessionGateway
