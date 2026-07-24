function normalizedIPv4(value: string): string {
  const octets = value.split('.')
  if (octets.length !== 4) return ''
  const normalized: string[] = []
  for (const octet of octets) {
    if (!/^(0|[1-9]\d{0,2})$/.test(octet)) return ''
    const number = Number(octet)
    if (number > 255) return ''
    normalized.push(String(number))
  }
  return normalized.join('.')
}

function normalizedIPv6(value: string): string {
  if (!value.includes(':') || value.includes('%')) return ''
  try {
    const hostname = new URL(`http://[${value}]/`).hostname
    return hostname.startsWith('[') && hostname.endsWith(']')
      ? hostname.slice(1, -1).toLocaleLowerCase()
      : hostname.toLocaleLowerCase()
  } catch {
    return ''
  }
}

export function normalizedIPAddress(value: string): string {
  const candidate = value.trim().toLocaleLowerCase()
  return normalizedIPv4(candidate) || normalizedIPv6(candidate)
}

export function isIPAddress(value: string): boolean {
  return Boolean(normalizedIPAddress(value))
}

export function isLoopbackAddress(value: string): boolean {
  const normalized = normalizedIPAddress(value)
  if (!normalized) return false
  if (normalized.includes(':')) return normalized === '::1'
  return normalized.startsWith('127.')
}
