import type { ListQuery, MessageQuery, ModemDeckGateway } from './gateway'
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

const devices: Device[] = [
  {
    imei: 'fixture-001',
    alias: 'Main cellular line',
    model: 'Fixture modem',
    firmware: 'Fixture 1.0',
    current_iccid: MAIN_ICCID,
    sim_inserted: true,
    signal_dbm: -71,
    sim: {
      iccid: MAIN_ICCID,
      imsi: '001010000000001',
      phone_number: '+12025550198',
      operator: 'Aurora Mobile',
      current_imei: 'fixture-001',
      reg_status: 1,
      reg_status_text: 'registered',
      lac: '1001',
      cell_id: '2002',
      apn: 'fixture',
      ims_status: 1,
      last_seen: '2026-07-23T12:00:00Z'
    }
  },
  {
    imei: 'fixture-002',
    alias: 'Travel cellular line',
    model: 'Fixture modem',
    firmware: 'Fixture 1.0',
    current_iccid: TRAVEL_ICCID,
    sim_inserted: true,
    signal_dbm: -83,
    sim: {
      iccid: TRAVEL_ICCID,
      imsi: '001020000000002',
      phone_number: '+1 202 555 0102',
      operator: 'Pine Wireless',
      current_imei: 'fixture-002',
      reg_status: 1,
      reg_status_text: 'registered',
      lac: '3003',
      cell_id: '4004',
      apn: 'fixture',
      ims_status: 1,
      last_seen: '2026-07-23T12:00:00Z'
    }
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

export function createFixtureGateway(): ModemDeckGateway {
  let sequence = 200
  let activeCall: CallSession | undefined
  let callPolls = 0

  return {
    interactive: true,

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
        lines: [
          {
            iccid: MAIN_ICCID,
            imsi: '001010000000001',
            phone_number: '+12025550198',
            operator: 'Aurora Mobile',
            device_imei: 'fixture-001',
            device_alias: 'Main cellular line'
          },
          {
            iccid: TRAVEL_ICCID,
            imsi: '001020000000002',
            phone_number: '+1 202 555 0102',
            operator: 'Pine Wireless',
            device_imei: 'fixture-002',
            device_alias: 'Travel cellular line'
          }
        ]
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
      const key = input.thread_key || threadKey(input.iccid, input.to)
      let thread = threads.find(item => item.key === key)
      if (!thread) {
        const contact = contacts.find(item => item.phones.some(phone => phone.number === input.to))
        thread = {
          key,
          imsi: '',
          iccid: input.iccid,
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
        iccid: input.iccid,
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

    async getCall(id: string): Promise<CallSession> {
      if (!activeCall || activeCall.id !== id) throw new ApiError('通话不存在', 404)
      callPolls += 1
      if (activeCall.phase === 'dialing' && callPolls >= 1) activeCall.phase = 'ringing'
      else if (activeCall.phase === 'ringing' && callPolls >= 2) {
        activeCall.phase = 'active'
        activeCall.active_at = '2026-07-23T12:05:04Z'
      }
      return clone(activeCall)
    },

    async startCall(lineKey: string, number: string): Promise<CallSession> {
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
        created_at: '2026-07-23T12:05:00Z'
      }
      return clone(activeCall)
    },

    async callAction(id: string, action: 'answer' | 'reject' | 'hangup'): Promise<CallSession> {
      if (!activeCall || activeCall.id !== id) throw new ApiError('通话不存在', 404)
      if (action === 'answer') {
        activeCall.phase = 'active'
        activeCall.active_at = '2026-07-23T12:05:04Z'
      } else {
        activeCall.phase = 'ended'
        activeCall.ended_at = '2026-07-23T12:08:00Z'
      }
      return clone(activeCall)
    },

    async listDevices(): Promise<Device[]> {
      return clone(devices)
    }
  }
}
