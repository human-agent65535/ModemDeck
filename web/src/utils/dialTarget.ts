export const MAX_DIAL_TARGET_RUNES = 64
export const MAX_DIAL_TARGET_DIGITS = 32

export type DialTargetError =
  | ''
  | 'required'
  | 'too_long'
  | 'invalid_character'
  | 'invalid_length'

export type DialTargetResult = {
  original: string
  normalized: string
  error: DialTargetError
}

const digit = /^[0-9]$/
const formattingSeparator = /^[\s().\-/]$/

function isControlCharacter(character: string): boolean {
  const codePoint = character.codePointAt(0) ?? 0
  return codePoint <= 0x1f || (codePoint >= 0x7f && codePoint <= 0x9f)
}

export function normalizeDialTarget(value: string): DialTargetResult {
  const original = value.trim()
  if (!original) return { original: '', normalized: '', error: 'required' }

  const characters = Array.from(original)
  if (characters.length > MAX_DIAL_TARGET_RUNES) {
    return { original, normalized: '', error: 'too_long' }
  }

  let normalized = ''
  let digitCount = 0
  for (const [index, character] of characters.entries()) {
    if (digit.test(character)) {
      normalized += character
      digitCount += 1
      continue
    }
    if (character === '+' && index === 0) {
      normalized += character
      continue
    }
    if (isControlCharacter(character) || !formattingSeparator.test(character)) {
      return { original, normalized: '', error: 'invalid_character' }
    }
  }

  if (digitCount === 0 || digitCount > MAX_DIAL_TARGET_DIGITS) {
    return { original, normalized: '', error: 'invalid_length' }
  }
  return { original, normalized, error: '' }
}
