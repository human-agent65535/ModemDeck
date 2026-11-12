// Base validators shared by the API parsers. Domain schemas stay in normalize.ts,
// contract.ts, and client.ts; the differences between them are explicit options here.

export type JsonRecord = Record<string, unknown>

/** Builds the error a failed check throws; client.ts passes one that makes an ApiError. */
export type ParseFailure = (message: string) => Error

const plainError: ParseFailure = message => new Error(message)

export function objectValue(
  value: unknown,
  path: string,
  fail: ParseFailure = plainError
): JsonRecord {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    throw fail(`${path} 必须是对象`)
  }
  return value as JsonRecord
}

/** Trimmed string, or a finite number as text; anything else reads as ''. */
export function stringValue(source: JsonRecord, key: string): string {
  const value = source[key]
  if (typeof value === 'string') return value.trim()
  if (typeof value === 'number' && Number.isFinite(value)) return String(value)
  return ''
}

export type RequiredStringOptions = {
  /** Accept a finite number and return it as text (normalize.ts). */
  acceptNumber?: boolean
  /** Accept an empty or blank string and return '' (some contract.ts fields). */
  allowEmpty?: boolean
}

export function requiredString(
  source: JsonRecord,
  path: string,
  key: string,
  options: RequiredStringOptions = {}
): string {
  const value = source[key]
  if (typeof value === 'string') {
    const trimmed = value.trim()
    if (trimmed || options.allowEmpty) return trimmed
  } else if (options.acceptNumber && typeof value === 'number' && Number.isFinite(value)) {
    return String(value)
  }
  throw new Error(`${path} 缺少 ${key}`)
}

export function requiredTimestamp(
  source: JsonRecord,
  path: string,
  key: string,
  options: RequiredStringOptions = {}
): string {
  const value = requiredString(source, path, key, options)
  if (!Number.isFinite(Date.parse(value))) throw new Error(`${path}.${key} 不是有效时间`)
  return value
}

export function requiredBoolean(
  source: JsonRecord,
  path: string,
  key: string,
  fail: ParseFailure = plainError
): boolean {
  const value = source[key]
  if (typeof value !== 'boolean') throw fail(`${path}.${key} 必须是布尔值`)
  return value
}
