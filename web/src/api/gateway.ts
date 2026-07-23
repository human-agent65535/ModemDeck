import type {
  BootstrapResponse,
  CallFilter,
  CallRecord,
  CallSession,
  Contact,
  ContactInput,
  Device,
  Message,
  MessageThread,
  SendMessageInput
} from './types'

export type ListQuery = {
  q?: string
}

export type MessageQuery = {
  iccid: string
  peer: string
}

export interface ModemDeckGateway {
  readonly interactive: boolean
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
