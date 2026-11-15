import TinyPinyin from 'tiny-pinyin'
import { phoneKeypad } from './phoneKeypad'

/**
 * Forms of a contact name the dialer searches: Chinese characters become pinyin syllables,
 * so letters match full pinyin from any syllable or the initials, and keypad digits match the
 * same forms through the letters printed on the keys. The iOS dialer applies the same rules.
 */
export type NameSearchKeys = {
  /** Each word or pinyin syllable joined from that point to the end of the name. */
  suffixes: string[]
  initials: string
  keypadSuffixes: string[]
  keypadInitials: string
}

const keypadDigit = new Map<string, string>(
  phoneKeypad.flatMap(key =>
    Array.from(key.letters.toLowerCase()).map(letter => [letter, key.digit] as [string, string])
  )
)

function keypadDigits(value: string): string {
  return Array.from(value, character => keypadDigit.get(character) ?? character).join('')
}

/** Lowercase ASCII letters and digits with tone marks and other diacritics removed. */
export function plainSearchText(value: string): string {
  return value
    .normalize('NFD')
    .replace(/[̀-ͯ]/g, '')
    .toLowerCase()
    .replace(/[^a-z0-9]/g, '')
}

function nameWords(name: string): string[] {
  // Without collation data for Chinese the name is still searched as written.
  const spaced = TinyPinyin.isSupported()
    ? TinyPinyin.parse(name)
        .map(token => (token.type === 2 ? ` ${token.target} ` : token.source))
        .join('')
    : name
  return spaced
    .normalize('NFD')
    .replace(/[̀-ͯ]/g, '')
    .toLowerCase()
    .split(/[^a-z0-9]+/)
    .filter(Boolean)
}

export function nameSearchKeys(name: string): NameSearchKeys {
  const words = nameWords(name)
  const suffixes = words.map((_, index) => words.slice(index).join(''))
  const initials = words.map(word => word.charAt(0)).join('')
  return {
    suffixes,
    initials,
    keypadSuffixes: suffixes.map(keypadDigits),
    keypadInitials: keypadDigits(initials)
  }
}

const cache = new Map<string, NameSearchKeys>()

/** Converting names to pinyin is slow enough to cache across keystrokes. */
export function cachedNameSearchKeys(name: string): NameSearchKeys {
  let keys = cache.get(name)
  if (!keys) {
    if (cache.size > 5000) cache.clear()
    keys = nameSearchKeys(name)
    cache.set(name, keys)
  }
  return keys
}

/** Typed letters, without spaces or tone marks, as full pinyin from a syllable or initials. */
export function matchesNameLetters(keys: NameSearchKeys, letters: string): boolean {
  return (
    letters.length > 0 &&
    (keys.suffixes.some(suffix => suffix.startsWith(letters)) || keys.initials.includes(letters))
  )
}

/** Keypad digits spelling the name with the letters on the keys. */
export function matchesNameKeypad(keys: NameSearchKeys, digits: string): boolean {
  return (
    digits.length > 0 &&
    (keys.keypadSuffixes.some(suffix => suffix.startsWith(digits)) ||
      keys.keypadInitials.includes(digits))
  )
}

export type DialSuggestion<Contact, Phone> = { contact: Contact; phone: Phone }

/**
 * Phones of contacts matching the entry, best first. Letters search the name as written,
 * as pinyin or as initials. Digits search phone numbers and, where the field is a dial pad
 * (`spellsNames`), the name spelled with the letters on the keys from two digits.
 */
export function dialSuggestions<
  Phone extends { number: string },
  Contact extends { display_name: string; phones: Phone[] }
>(
  contacts: readonly Contact[],
  value: string,
  options: { spellsNames: boolean; minimumNumberDigits: number; limit: number }
): DialSuggestion<Contact, Phone>[] {
  const text = value.trim()
  if (!text || options.limit <= 0) return []
  const searchesName = /\p{L}/u.test(text)
  const digits = text.replace(/\D/g, '')
  const keypadEntry = options.spellsNames && !searchesName && !/[+*#]/.test(text)
  if (!searchesName && digits.length < Math.min(2, options.minimumNumberDigits)) return []
  const lowered = text.toLocaleLowerCase()
  const letters = plainSearchText(text)

  const nameMatches: DialSuggestion<Contact, Phone>[] = []
  const numberMatches: DialSuggestion<Contact, Phone>[] = []
  for (const contact of contacts) {
    const keys = cachedNameSearchKeys(contact.display_name)
    const nameMatched = searchesName
      ? contact.display_name.toLocaleLowerCase().includes(lowered) || matchesNameLetters(keys, letters)
      : keypadEntry && matchesNameKeypad(keys, digits)
    for (const phone of contact.phones) {
      if (nameMatched) {
        nameMatches.push({ contact, phone })
      } else if (!searchesName && digits.length >= options.minimumNumberDigits && phone.number.replace(/\D/g, '').includes(digits)) {
        numberMatches.push({ contact, phone })
      }
    }
    if (nameMatches.length >= options.limit) break
  }
  return [...nameMatches, ...numberMatches].slice(0, options.limit)
}
