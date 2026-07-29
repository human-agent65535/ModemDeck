import {
  parsePhoneNumberFromString,
  type CountryCode
} from 'libphonenumber-js/min'
import type { Contact, ContactInput } from '../api/types'

export type TransferPhone = {
  label: string
  number: string
  region?: string
}

export type TransferContact = {
  display_name: string
  avatar?: string
  notes?: string
  phones: TransferPhone[]
}

export type ContactImportPlan = {
  existing?: Contact
  input?: ContactInput
  conflict: boolean
}

const supportedImageTypes = new Set(['jpeg', 'jpg', 'png', 'webp'])

function normalizedRegion(value: string): CountryCode | undefined {
  const region = value.trim().toUpperCase()
  return /^[A-Z]{2}$/.test(region) ? (region as CountryCode) : undefined
}

export function normalizedTransferPhone(
  value: string,
  defaultRegion = ''
): TransferPhone | undefined {
  const source = value.trim().replace(/^tel:/i, '')
  if (!source) return undefined
  const parsed = parsePhoneNumberFromString(source, normalizedRegion(defaultRegion))
  if (!parsed?.isPossible()) return undefined
  return {
    label: '',
    number: parsed.number,
    region: parsed.country
  }
}

function phoneKey(value: string, defaultRegion = ''): string {
  return normalizedTransferPhone(value, defaultRegion)?.number || ''
}

export function normalizeTransferContact(
  contact: TransferContact,
  defaultRegion = ''
): TransferContact | undefined {
  const displayName = contact.display_name.trim()
  if (!displayName) return undefined

  const seen = new Set<string>()
  const phones: TransferPhone[] = []
  for (const phone of contact.phones) {
    const normalized = normalizedTransferPhone(phone.number, phone.region || defaultRegion)
    if (!normalized || seen.has(normalized.number)) continue
    seen.add(normalized.number)
    phones.push({
      label: phone.label.trim() || 'Phone',
      number: normalized.number,
      region: normalized.region
    })
  }
  if (phones.length === 0) return undefined

  return {
    display_name: displayName,
    avatar: contact.avatar?.trim() || undefined,
    notes: contact.notes?.trim() || undefined,
    phones
  }
}

function contactPhoneKey(phone: Contact['phones'][number]): string {
  return phoneKey(phone.normalized_number || phone.number, phone.region)
}

export function planContactImport(
  transfer: TransferContact,
  contacts: Contact[],
  defaultRegion = ''
): ContactImportPlan {
  const normalized = normalizeTransferContact(transfer, defaultRegion)
  if (!normalized) return { conflict: false }

  const importedKeys = new Set(normalized.phones.map(phone => phone.number))
  const matches = contacts.filter(contact =>
    contact.phones.some(phone => importedKeys.has(contactPhoneKey(phone)))
  )
  if (matches.length > 1) return { conflict: true }

  const existing = matches[0]
  const phones: ContactInput['phones'] = []
  const seen = new Set<string>()
  for (const phone of existing?.phones || []) {
    const key = contactPhoneKey(phone)
    if (!key || seen.has(key)) continue
    seen.add(key)
    phones.push({
      id: phone.id,
      label: phone.label,
      number: phone.normalized_number || phone.number,
      region: phone.region,
      primary: phone.primary
    })
  }
  for (const phone of normalized.phones) {
    if (seen.has(phone.number)) continue
    seen.add(phone.number)
    phones.push({
      label: phone.label,
      number: phone.number,
      region: phone.region,
      primary: phones.length === 0
    })
  }
  if (!phones.some(phone => phone.primary) && phones[0]) phones[0].primary = true

  return {
    existing,
    conflict: false,
    input: {
      display_name: normalized.display_name,
      avatar: normalized.avatar || existing?.avatar,
      favorite: existing?.favorite || false,
      notes: normalized.notes || existing?.notes,
      preferred_line_id: existing?.preferred_line_id,
      revision: existing?.revision,
      phones
    }
  }
}

function unfoldVCard(source: string): string[] {
  return source
    .replace(/\r\n/g, '\n')
    .replace(/\r/g, '\n')
    .replace(/\n[ \t]/g, '')
    .split('\n')
}

function unescapeVCardText(value: string): string {
  return value.replace(/\\([nN,;\\])/g, (_, escaped: string) => {
    if (escaped === 'n' || escaped === 'N') return '\n'
    return escaped
  })
}

function structuredVCardName(value: string): string {
  const [family = '', given = '', additional = '', prefix = '', suffix = ''] =
    value.split(';').map(unescapeVCardText)
  return [prefix, given, additional, family, suffix].filter(Boolean).join(' ')
}

function propertyParts(line: string): {
  name: string
  parameters: string
  value: string
} | undefined {
  const separator = line.indexOf(':')
  if (separator < 0) return undefined
  const header = line.slice(0, separator)
  const [qualifiedName = '', ...parameterParts] = header.split(';')
  const name = (qualifiedName.split('.').at(-1) || '').toUpperCase()
  if (!name) return undefined
  return {
    name,
    parameters: parameterParts.join(';'),
    value: line.slice(separator + 1)
  }
}

function vCardPhoneLabel(parameters: string): string {
  const values = parameters
    .split(/[;,]/)
    .map(value => value.replace(/^TYPE=/i, '').trim().toUpperCase())
  if (values.includes('CELL')) return 'Mobile'
  if (values.includes('HOME')) return 'Home'
  if (values.includes('WORK')) return 'Work'
  return 'Phone'
}

function vCardPhoto(parameters: string, value: string): string | undefined {
  if (!/(?:^|;)ENCODING=(?:B|BASE64)(?:;|$)/i.test(parameters)) return undefined
  const match = parameters.match(/(?:^|;)TYPE=([^;]+)/i)
  const rawType = (match?.[1] || 'jpeg').split(',')[0]?.trim().toLowerCase() || 'jpeg'
  const imageType = rawType === 'jpg' ? 'jpeg' : rawType
  if (!supportedImageTypes.has(rawType) || !/^[A-Za-z0-9+/=]+$/.test(value)) {
    return undefined
  }
  return `data:image/${imageType};base64,${value}`
}

export function parseVCard(
  source: string,
  defaultRegion = ''
): TransferContact[] {
  const contacts: TransferContact[] = []
  let current:
    | {
        fn: string
        structuredName: string
        notes: string
        avatar?: string
        phones: TransferPhone[]
      }
    | undefined

  for (const line of unfoldVCard(source)) {
    const normalizedLine = line.trim()
    if (/^BEGIN:VCARD$/i.test(normalizedLine)) {
      current = { fn: '', structuredName: '', notes: '', phones: [] }
      continue
    }
    if (/^END:VCARD$/i.test(normalizedLine)) {
      if (!current) continue
      const normalized = normalizeTransferContact(
        {
          display_name: current.fn || current.structuredName,
          avatar: current.avatar,
          notes: current.notes,
          phones: current.phones
        },
        defaultRegion
      )
      if (normalized) contacts.push(normalized)
      current = undefined
      continue
    }
    if (!current) continue
    const property = propertyParts(line)
    if (!property) continue
    switch (property.name) {
      case 'FN':
        current.fn = unescapeVCardText(property.value)
        break
      case 'N':
        current.structuredName = structuredVCardName(property.value)
        break
      case 'TEL':
        current.phones.push({
          label: vCardPhoneLabel(property.parameters),
          number: unescapeVCardText(property.value)
        })
        break
      case 'NOTE':
        current.notes = unescapeVCardText(property.value)
        break
      case 'PHOTO':
        current.avatar = vCardPhoto(property.parameters, property.value)
        break
    }
  }
  return contacts
}

function escapeVCardText(value: string): string {
  return value
    .replace(/\\/g, '\\\\')
    .replace(/\r?\n/g, '\\n')
    .replace(/,/g, '\\,')
    .replace(/;/g, '\\;')
}

function photoLine(avatar: string | undefined): string {
  const match = avatar?.match(/^data:image\/(gif|jpe?g|png|webp);base64,(.+)$/i)
  if (!match) return ''
  const type = (match[1] || 'jpeg').toUpperCase().replace('JPG', 'JPEG')
  return `PHOTO;ENCODING=b;TYPE=${type}:${match[2]}`
}

export function serializeContactsToVCard(contacts: Contact[]): string {
  const cards = contacts.map(contact => {
    const lines = [
      'BEGIN:VCARD',
      'VERSION:3.0',
      `FN:${escapeVCardText(contact.display_name)}`,
      `N:;${escapeVCardText(contact.display_name)};;;`
    ]
    for (const phone of contact.phones) {
      const type = /mobile|cell|手机|移动/i.test(phone.label)
        ? 'CELL'
        : /home|住宅|家庭/i.test(phone.label)
          ? 'HOME'
          : /work|office|公司|工作/i.test(phone.label)
            ? 'WORK'
            : 'VOICE'
      lines.push(`TEL;TYPE=${type}:${phone.normalized_number || phone.number}`)
    }
    if (contact.notes) lines.push(`NOTE:${escapeVCardText(contact.notes)}`)
    const photo = photoLine(contact.avatar)
    if (photo) lines.push(photo)
    lines.push('END:VCARD')
    return lines.join('\r\n')
  })
  return `${cards.join('\r\n')}\r\n`
}
