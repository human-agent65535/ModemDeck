import { compactPhoneSyntax } from './lineIdentity'

export type CommunicationChannel = 'call' | 'message' | 'recording'
export type CommunicationAvatarFallback =
  | 'initials'
  | 'person'
  | CommunicationChannel

type CommunicationAvatarIdentity = {
  channel: CommunicationChannel
  name: string
  address: string
  contactBound: boolean
}

function comparisonKey(value: string): string {
  const source = value.trim().normalize('NFKC').toLocaleLowerCase()
  if (!source) return ''
  if (!/^[+\d\s().\-/]+$/.test(source)) return source.replace(/\s+/g, ' ')

  return compactPhoneSyntax(source, { internationalDoubleZero: true })
}

export function communicationAvatarFallback(
  identity: CommunicationAvatarIdentity
): CommunicationAvatarFallback {
  if (!identity.contactBound) return identity.channel
  const nameKey = comparisonKey(identity.name)
  const addressKey = comparisonKey(identity.address)
  return nameKey && nameKey !== addressKey ? 'initials' : 'person'
}

export function communicationAvatarPaletteKey(
  identity: CommunicationAvatarIdentity,
  fallback = communicationAvatarFallback(identity)
): string {
  if (fallback === 'initials') return identity.name.trim()
  return comparisonKey(identity.address) || identity.channel
}
