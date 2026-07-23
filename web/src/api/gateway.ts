import type {
  BootstrapResponse,
  CallFilter,
  CallRecord,
  CallSession,
  Contact,
  ContactInput,
  Device,
  LoginInput,
  Message,
  MessageThread,
  SendMessageInput,
  SessionResponse
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
  createContact?(input: ContactInput): Promise<Contact>
  updateContact?(id: string, input: ContactInput): Promise<Contact>
  deleteContact?(id: string, revision?: number): Promise<void>
  sendMessage?(input: SendMessageInput): Promise<Message>
  getCall?(id: string): Promise<CallSession>
  startCall?(lineKey: string, number: string): Promise<CallSession>
  callAction?(id: string, action: 'answer' | 'reject' | 'hangup'): Promise<CallSession>
}

export type ConfiguredModemDeckGateway = ModemDeckGateway & {
  readonly interactions: GatewayInteractions
} & SessionGateway
