import { resolvedLocale, translate } from '../i18n'

export function initials(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean)
  if (parts.length === 0) return '#'
  if (parts.length === 1) return Array.from(parts[0] || '#').slice(0, 2).join('').toUpperCase()
  return `${Array.from(parts[0] || '#')[0] || ''}${Array.from(parts.at(-1) || '#')[0] || ''}`.toUpperCase()
}

import {
  AsYouType,
  parsePhoneNumberFromString,
  type CountryCode
} from 'libphonenumber-js/min'

export function formatRelativeDate(value: string): string {
  const date = new Date(value)
  if (!Number.isFinite(date.getTime())) return value
  const now = new Date()
  const sameDay = date.toDateString() === now.toDateString()
  const locale = resolvedLocale()
  if (sameDay) {
    return new Intl.DateTimeFormat(locale, {
      hour: '2-digit',
      minute: '2-digit'
    }).format(date)
  }
  const yesterday = new Date(now)
  yesterday.setDate(now.getDate() - 1)
  if (date.toDateString() === yesterday.toDateString()) return translate('common.yesterday')
  if (date.getFullYear() === now.getFullYear()) {
    return new Intl.DateTimeFormat(locale, { month: 'numeric', day: 'numeric' }).format(date)
  }
  return new Intl.DateTimeFormat(locale, {
    year: 'numeric',
    month: 'numeric',
    day: 'numeric'
  }).format(date)
}

/** Day label for conversation separators: Today, Yesterday, or a full date. */
export function formatMessageDay(value: string): string {
  const date = new Date(value)
  if (!Number.isFinite(date.getTime())) return value
  const now = new Date()
  if (date.toDateString() === now.toDateString()) return translate('common.today')
  const yesterday = new Date(now)
  yesterday.setDate(now.getDate() - 1)
  if (date.toDateString() === yesterday.toDateString()) return translate('common.yesterday')
  return new Intl.DateTimeFormat(resolvedLocale(), {
    weekday: 'short',
    month: 'short',
    day: 'numeric',
    ...(date.getFullYear() === now.getFullYear() ? {} : { year: 'numeric' })
  }).format(date)
}

/** Clock time shown inside a bubble once the day is shown by its separator. */
export function formatMessageTime(value: string): string {
  const date = new Date(value)
  if (!Number.isFinite(date.getTime())) return value
  return new Intl.DateTimeFormat(resolvedLocale(), { hour: '2-digit', minute: '2-digit' }).format(date)
}

const DATE_TIME_STYLES = {
  /** Default list/detail timestamp: `Mar 5, 2026, 09:41`. */
  default: {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit'
  },
  /** All-numeric date and time, used for certificate validity. */
  numeric: {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit'
  },
  /** 24-hour log stamp with seconds and day, no year. */
  log: {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false
  },
  /** 24-hour log stamp with seconds only. */
  logTime: {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false
  },
  /** Locale medium date with short time. */
  medium: {
    dateStyle: 'medium',
    timeStyle: 'short'
  }
} satisfies Record<string, Intl.DateTimeFormatOptions>

export type DateTimeStyle = keyof typeof DATE_TIME_STYLES

/** Formats a timestamp in a named style; an unparseable value is returned unchanged. */
export function formatDateTime(value: string, style: DateTimeStyle = 'default'): string {
  const date = new Date(value)
  if (!Number.isFinite(date.getTime())) return value
  return new Intl.DateTimeFormat(resolvedLocale(), DATE_TIME_STYLES[style]).format(date)
}

/**
 * Length of a finished call or recording: `42 s` under a minute, then `m:ss`.
 * Not a clock; live calls use formatCallClock.
 */
export function formatCompletedDuration(seconds: number): string {
  const safe = Math.max(0, Math.floor(seconds))
  const minutes = Math.floor(safe / 60)
  const remainder = safe % 60
  return minutes > 0
    ? `${minutes}:${String(remainder).padStart(2, '0')}`
    : translate('common.durationSeconds', { count: remainder })
}

function formatClock(safe: number, padMinutes: boolean): string {
  const hours = Math.floor(safe / 3600)
  const minuteValue = Math.floor((safe % 3600) / 60)
  const remainder = String(safe % 60).padStart(2, '0')
  if (hours > 0) return `${hours}:${String(minuteValue).padStart(2, '0')}:${remainder}`
  return `${padMinutes ? String(minuteValue).padStart(2, '0') : minuteValue}:${remainder}`
}

/** Live call clock shared with iOS: `mm:ss`, then `h:mm:ss` past an hour. */
export function formatCallClock(seconds: number): string {
  return formatClock(Math.max(0, Math.floor(seconds)), true)
}

/** Live call clock for a call that became active at `startedAt` and runs until `endMs`. */
export function formatCallElapsed(startedAt: string, endMs: number): string {
  return formatCallClock(Math.max(0, Math.floor((endMs - Date.parse(startedAt)) / 1000)))
}

/** Media player position: `m:ss` without a leading zero, `h:mm:ss` past an hour; non-finite reads as 0. */
export function formatPlaybackTime(seconds: number): string {
  return formatClock(Number.isFinite(seconds) ? Math.max(0, Math.floor(seconds)) : 0, false)
}

export type ByteSizeStyle = 'adaptive' | 'recording' | 'roundedKiB'

/**
 * Byte counts in three established styles:
 * - adaptive: 1024 steps up to PB, 0-2 decimals by magnitude (traffic).
 * - recording: KB or MB with one decimal (recording files).
 * - roundedKiB: whole KiB rounded up (selected upload files).
 */
export function formatBytes(bytes: number, style: ByteSizeStyle = 'adaptive'): string {
  if (bytes < 1024) return `${bytes} B`
  if (style === 'roundedKiB') return `${Math.ceil(bytes / 1024)} KiB`
  if (style === 'recording') {
    if (bytes < 1024 * 1024) return `${Math.max(0.1, bytes / 1024).toFixed(1)} KB`
    return `${Math.max(0.1, bytes / (1024 * 1024)).toFixed(1)} MB`
  }
  const units = ['KB', 'MB', 'GB', 'TB', 'PB']
  let value = bytes / 1024
  let unit = units[0]
  for (let index = 1; index < units.length && value >= 1024; index += 1) {
    value /= 1024
    unit = units[index]
  }
  const precision = value >= 100 ? 0 : value >= 10 ? 1 : 2
  return `${value.toFixed(precision)} ${unit}`
}

export function primaryPhone(phones: Array<{ number: string; primary: boolean }>): string {
  return phones.find(phone => phone.primary)?.number || phones[0]?.number || ''
}

export function phoneDestination(phone: {
  number: string
  normalized_number?: string
}): string {
  return phone.normalized_number?.trim() || phone.number.trim()
}

export function primaryPhoneDestination(
  phones: Array<{ number: string; normalized_number?: string; primary: boolean }>
): string {
  const phone = phones.find(item => item.primary) || phones[0]
  return phone ? phoneDestination(phone) : ''
}

export function formatPhoneNumber(value: string, homeRegion = ''): string {
  const original = value.trim()
  if (!original) return ''
  const normalizedRegion = homeRegion.trim().toUpperCase()
  const region: CountryCode | undefined = /^[A-Z]{2}$/.test(normalizedRegion)
    ? (normalizedRegion as CountryCode)
    : undefined
  const parsed = parsePhoneNumberFromString(original, region)
  if (!parsed?.isPossible()) return original
  return region && parsed.country === region
    ? parsed.formatNational()
    : parsed.formatInternational()
}

/**
 * Groups a dial-pad number as it is typed (202 555 0107, +1 202 555 0107), as phone apps do.
 * Only plain numbers are grouped; service codes with * or # stay exactly as entered.
 */
export function formatDialInput(value: string, homeRegion = ''): string {
  if (!/^\+?\d{4,}$/.test(value)) return value
  const normalizedRegion = homeRegion.trim().toUpperCase()
  const region = /^[A-Z]{2}$/.test(normalizedRegion) ? (normalizedRegion as CountryCode) : undefined
  return new AsYouType(region).input(value)
}

/** True when the dial field holds a number or service code, not a contact name search. */
export function isDialEntry(value: string): boolean {
  return /^\+?[\d*#]*$/.test(stripDialFormatting(value))
}

/** Removes the grouping that formatDialInput adds, so the draft keeps only what is dialed. */
export function stripDialFormatting(value: string): string {
  return value.replace(/[\s().-]/g, '')
}
