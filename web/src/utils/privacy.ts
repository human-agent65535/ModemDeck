export function maskIdentifier(value?: string): string {
  const normalized = value?.trim() || ''
  if (!normalized) return '—'
  if (normalized.length <= 8) return normalized
  return `••••${Array.from(normalized).slice(-4).join('')}`
}
