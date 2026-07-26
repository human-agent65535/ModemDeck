import type { LineSummary } from '../api/types'

export type LineTone = {
  foreground: string
  background: string
  border: string
}

const LINE_TONES: readonly LineTone[] = [
  { foreground: '#075e54', background: '#e0f2ef', border: '#a7d8d1' },
  { foreground: '#20558c', background: '#e7f0fa', border: '#b8d0e9' },
  { foreground: '#7a4b00', background: '#fff2d6', border: '#e9cf93' },
  { foreground: '#8a3448', background: '#fae9ed', border: '#e4b9c3' },
  { foreground: '#3e6a25', background: '#eaf3e4', border: '#bed4ae' },
  { foreground: '#5c4b8a', background: '#efebf8', border: '#cbc1e2' }
]

function stableHash(value: string): number {
  let hash = 2166136261
  for (const character of value) {
    hash ^= character.codePointAt(0) || 0
    hash = Math.imul(hash, 16777619)
  }
  return hash >>> 0
}

export function lineTone(
  line: Pick<LineSummary, 'id' | 'iccid' | 'line_label'>,
  fallback = ''
): LineTone {
  const stableKey =
    line.id?.trim() ||
    line.iccid.trim() ||
    line.line_label.trim() ||
    fallback.trim() ||
    'line'
  return LINE_TONES[stableHash(stableKey) % LINE_TONES.length] ?? LINE_TONES[0]!
}
