export type CommunicationAddressKind =
  | 'subscriber'
  | 'short_code'
  | 'alphanumeric'
  | 'unknown'

function compactPhoneSyntax(value: string): string {
  return value.trim().replace(/[\s().\-/]/g, '')
}

export function communicationAddressKind(value: string): CommunicationAddressKind {
  const source = value.trim()
  if (!source) return 'unknown'

  const compact = compactPhoneSyntax(source)
  if (/^\+[1-9]\d{7,14}$/.test(compact)) return 'subscriber'
  if (/^\d{2,6}$/.test(compact)) return 'short_code'

  const characters = Array.from(source)
  if (
    characters.length <= 32 &&
    /\p{L}/u.test(source) &&
    /^[\p{L}\p{N} ._-]+$/u.test(source)
  ) {
    return 'alphanumeric'
  }
  return 'unknown'
}

export function isContactPhoneCandidate(value: string): boolean {
  const source = value.trim()
  if (!source || !/^[+\d\s().\-/]+$/.test(source)) return false

  const compact = compactPhoneSyntax(source)
  const digits = compact.replace(/\D/g, '')
  return (
    digits.length >= 7 &&
    digits.length <= 15 &&
    (/^\+[1-9]\d+$/.test(compact) || /^\d+$/.test(compact))
  )
}

export function isOneWayMessageSender(value: string): boolean {
  return communicationAddressKind(value) === 'alphanumeric'
}
