import type { LineColorPresetID, LineSummary } from '../api/types'

export type LineTone = {
  foreground: string
  background: string
  border: string
  dot: string
}

export type LineTonePreset = LineTone & {
  id: LineColorPresetID
}

// Colors live in style.css (--line-<id>-fg/bg/border/dot) so a theme can
// redefine them; these values are var() references usable in inline styles.
function linePreset(id: LineColorPresetID): LineTonePreset {
  return {
    id,
    foreground: `var(--line-${id}-fg)`,
    background: `var(--line-${id}-bg)`,
    border: `var(--line-${id}-border)`,
    dot: `var(--line-${id}-dot)`
  }
}

export const LINE_TONE_PRESETS: readonly LineTonePreset[] = [
  linePreset('teal'),
  linePreset('blue'),
  linePreset('indigo'),
  linePreset('violet'),
  linePreset('green'),
  linePreset('amber'),
  linePreset('orange'),
  linePreset('red')
]

// Automatic assignment avoids teal/green (brand accent and success) and red
// (danger) so an unlabeled line never reads as a status color.
const AUTO_LINE_TONE_PRESETS: readonly LineTonePreset[] = [
  LINE_TONE_PRESETS[1]!,
  LINE_TONE_PRESETS[5]!,
  LINE_TONE_PRESETS[3]!,
  LINE_TONE_PRESETS[6]!,
  LINE_TONE_PRESETS[2]!
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
