import { ApiError, type ResourceStatus } from '../api/types'
import { i18n, translate } from '../i18n'

// Codes whose meaning is the same wherever they surface; translated under `errors.<key>`.
const NOT_FOUND_CODES = new Set([
  'not_found',
  'contact_not_found',
  'message_thread_not_found',
  'call_not_found',
  'recording_not_found',
  'user_not_found',
  'line_not_found',
  'test_call_not_found'
])

// A bare `conflict` is the server refusing an operation in the current state:
// a line that already has a call, a SIM that changed, a device that moved on.
// Only `revision_conflict` means someone else edited the same record.
const CODE_ALIASES: Record<string, string> = {
  conflict: 'operation_conflict'
}

function translatedCode(code: string): string {
  const key = NOT_FOUND_CODES.has(code) ? 'not_found' : CODE_ALIASES[code] || code
  return i18n.global.te(`errors.${key}`) ? translate(`errors.${key}`) : ''
}

/**
 * An error the client raises with text already written for the person, such as
 * a translated validation message. userErrorMessage shows its message as is.
 * The name stays `Error` so console output that logs error names is unchanged.
 */
export class LocalizedError extends Error {}

/**
 * A message a person can act on. Server and client error text is written for
 * developers and is often in another language, so known codes and transport
 * failures are translated and anything else uses the caller's localized fallback.
 */
export function userErrorMessage(cause: unknown, fallback: string): string {
  if (cause instanceof LocalizedError) return cause.message
  if (cause instanceof ApiError) {
    const byCode = cause.code ? translatedCode(cause.code) : ''
    if (byCode) return byCode
    if (cause.status === 401) return translate('errors.authentication_required')
    if (cause.status === 403) return translate('errors.forbidden')
    if (cause.status >= 500) return translate('errors.server')
    return fallback
  }
  // fetch() rejects with a TypeError when the server cannot be reached.
  if (cause instanceof TypeError) return translate('errors.network')
  return fallback
}

/** A 403 shows the resource's own forbidden text, whatever code the server sent. */
export function forbiddenOrUserErrorMessage(
  cause: unknown,
  fallback: string,
  forbidden: string
): string {
  if (cause instanceof ApiError && cause.status === 403) return forbidden
  return userErrorMessage(cause, fallback)
}

export function errorResourceStatus(cause: unknown): ResourceStatus {
  return cause instanceof ApiError && cause.status === 403 ? 'forbidden' : 'error'
}
