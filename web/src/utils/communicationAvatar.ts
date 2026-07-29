import {
  communicationAddressKind,
  isContactPhoneCandidate
} from './communicationAddress'

export type CommunicationChannel = 'call' | 'message'
export type CommunicationAvatarFallback =
  | 'initials'
  | 'person'
  | 'service'
  | 'unknown'

type CommunicationAvatarIdentity = {
  channel: CommunicationChannel
  name: string
  address: string
}

function comparisonKey(value: string): string {
  const source = value.trim().normalize('NFKC').toLocaleLowerCase()
  if (!source) return ''
  if (!/^[+\d\s().\-/]+$/.test(source)) return source.replace(/\s+/g, ' ')

  let compact = source.replace(/[\s().\-/]/g, '')
  if (/^00[1-9]\d+$/.test(compact)) compact = `+${compact.slice(2)}`
  return compact
}

function hasNamedIdentity(name: string, address: string): boolean {
  const nameKey = comparisonKey(name)
  const addressKey = comparisonKey(address)
  return Boolean(nameKey && addressKey && nameKey !== addressKey)
}

export function communicationAvatarFallback(
  identity: CommunicationAvatarIdentity
): CommunicationAvatarFallback {
  const address = identity.address.trim()
  if (!address) return 'unknown'
  if (hasNamedIdentity(identity.name, address)) return 'initials'

  const kind = communicationAddressKind(address)
  const phoneAddress =
    isContactPhoneCandidate(address) || kind === 'subscriber'

  if (identity.channel === 'call') {
    if (phoneAddress) return 'person'
    return kind === 'short_code' ? 'service' : 'unknown'
  }
  if (kind === 'alphanumeric') return 'initials'
  return phoneAddress ? 'person' : 'service'
}

export function communicationAvatarPaletteKey(
  identity: CommunicationAvatarIdentity,
  fallback = communicationAvatarFallback(identity)
): string {
  if (fallback === 'unknown') return 'anonymous'
  if (fallback === 'person') return comparisonKey(identity.address)
  return identity.name.trim() || identity.address.trim()
}
