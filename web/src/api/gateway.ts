import type {
  BootstrapResponse,
  CallAction,
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

export type ListQuery = {
  q?: string
}

export type MessageQuery = {
  iccid: string
  peer: string
}

export type GatewayInteractions = Readonly<{
  contacts: boolean
  message: boolean
  dial: boolean
  telegram: boolean
}>

export interface SessionGateway {
  getSession(): Promise<SessionResponse>
  login(input: LoginInput): Promise<SessionResponse>
  logout(): Promise<void>
}

export interface ModemDeckGateway {
  readonly interactions?: GatewayInteractions
  getBootstrap(): Promise<BootstrapResponse>
  listContacts(query?: ListQuery): Promise<Contact[]>
  listThreads(query?: ListQuery): Promise<MessageThread[]>
  listMessages(query: MessageQuery): Promise<Message[]>
  listCalls(filter?: CallFilter, query?: ListQuery): Promise<CallRecord[]>
  listDevices(): Promise<Device[]>
  getGlobalCallSettings(): Promise<GlobalCallSettings>
  updateGlobalCallSettings(input: UpdateGlobalCallSettingsInput): Promise<GlobalCallSettings>
  getDeviceConfiguration(lineID: string): Promise<DeviceConfiguration>
  updateDeviceConfiguration(
    lineID: string,
    input: UpdateDeviceConfigurationInput
  ): Promise<DeviceConfiguration>
  createContact?(input: ContactInput): Promise<Contact>
  updateContact?(id: string, input: ContactInput): Promise<Contact>
  deleteContact?(id: string, revision?: number): Promise<void>
  sendMessage(input: SendMessageInput): Promise<Message>
  getActiveCalls(): Promise<CallSession[]>
  startCall(lineKey: string, number: string, recordingEnabled?: boolean): Promise<CallSession>
  callAction(id: string, action: CallAction): Promise<CallSession>
  sendDTMF(id: string, digit: string): Promise<CallSession>
  exchangeCallMedia(id: string, offerSDP: string): Promise<string>
  getRecordingSettings(): Promise<RecordingSettings>
  updateRecordingSettings(settings: RecordingSettings): Promise<RecordingSettings>
  setCallRecording(id: string, enabled: boolean): Promise<CallRecordingState>
  listCallRecordings(id: string): Promise<CallRecording[]>
  listTelegramUnits(): Promise<TelegramUnit[]>
  createTelegramUnit(input: TelegramUnitInput): Promise<TelegramUnit>
  updateTelegramUnit(id: string, input: TelegramUnitInput): Promise<TelegramUnit>
  deleteTelegramUnit(id: string, revision: number): Promise<void>
}

export type ConfiguredModemDeckGateway = ModemDeckGateway & {
  readonly interactions: GatewayInteractions
} & SessionGateway
