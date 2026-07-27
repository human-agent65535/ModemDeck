import type { LineColorPresetID, LineSummary } from '../api/types'

export type LineTone = {
  foreground: string
  background: string
  border: string
}

export type LineTonePreset = LineTone & {
  id: LineColorPresetID
}

export const LINE_TONE_PRESETS: readonly LineTonePreset[] = [
  { id: 'teal', foreground: '#075e54', background: '#d7f1ed', border: '#78c4ba' },
  { id: 'blue', foreground: '#1e4f8a', background: '#dfeafa', border: '#87add6' },
  { id: 'indigo', foreground: '#343d91', background: '#e2e5fb', border: '#9199dc' },
  { id: 'violet', foreground: '#6b3287', background: '#f0e1f8', border: '#bf8bd7' },
  { id: 'green', foreground: '#35651e', background: '#e2f1d9', border: '#91c277' },
  { id: 'amber', foreground: '#765000', background: '#fff0bd', border: '#e2ba55' },
  { id: 'orange', foreground: '#8b3e08', background: '#ffe3cf', border: '#e9a16d' },
  { id: 'red', foreground: '#982d22', background: '#f8dad5', border: '#de8072' }
]

const AUTO_LINE_TONE_PRESETS: readonly LineTonePreset[] = [
  LINE_TONE_PRESETS[0]!,
  LINE_TONE_PRESETS[1]!,
  LINE_TONE_PRESETS[5]!,
  LINE_TONE_PRESETS[7]!,
  LINE_TONE_PRESETS[4]!,
  LINE_TONE_PRESETS[3]!
]

function stableHash(value: string): number {
  let hash = 2166136261
  for (const character of value) {
    hash ^= character.codePointAt(0) || 0
    hash = Math.imul(hash, 16777619)
  }
  return hash >>> 0
}

export function lineTonePreset(
  line: Pick<LineSummary, 'id' | 'line_color'>
): LineTonePreset {
  const selected = LINE_TONE_PRESETS.find(preset => preset.id === line.line_color)
  if (selected) return selected

  const stableKey = line.id.trim() || 'line'
  return (
    AUTO_LINE_TONE_PRESETS[stableHash(stableKey) % AUTO_LINE_TONE_PRESETS.length] ??
    AUTO_LINE_TONE_PRESETS[0]!
  )
}

export function lineTone(
  line: Pick<LineSummary, 'id' | 'line_color'>
): LineTone {
  return lineTonePreset(line)
}
