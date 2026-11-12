import type { LineSummary } from '../api/types'
import { translate } from '../i18n'

export type LineTagLine = Pick<LineSummary, 'id' | 'line_label' | 'line_color'>

/**
 * Removes the punctuation people type inside a phone number: whitespace, ( ) . - /.
 * `internationalDoubleZero` also rewrites a leading `00` exit code as `+`; only
 * avatar comparison does that, address matching keeps `0049…` and `+49…` apart.
 */
export function compactPhoneSyntax(
  value: string,
  options: { internationalDoubleZero?: boolean } = {}
): string {
  const compact = value.trim().replace(/[\s().\-/]/g, '')
  if (options.internationalDoubleZero && /^00[1-9]\d+$/.test(compact)) {
    return `+${compact.slice(2)}`
  }
  return compact
}

export function normalizedPhoneIdentity(value: string | undefined): string {
  const source = value?.trim() || ''
  if (!source) return ''
  const compact = compactPhoneSyntax(source)
  if (/^\+[1-9]\d{7,14}$/.test(compact)) return compact
  return source.toLocaleLowerCase()
}

export function phoneIdentitiesMatch(
  left: string | undefined,
  right: string | undefined
): boolean {
  const leftIdentity = normalizedPhoneIdentity(left)
  return leftIdentity !== '' && leftIdentity === normalizedPhoneIdentity(right)
}

export function lineTagLine(
  line: LineSummary | undefined,
  lineID: string
): LineTagLine {
  if (line) return line
  return {
    id: lineID.trim(),
    line_label: ''
  }
}

export function lineTagFallback(
  line: LineSummary | undefined,
  lines: LineSummary[],
  defaultLineID: string,
  lineID?: string
): string {
  if (line) {
    const moduleName = line.device_name.trim() || line.model?.trim()
    if (moduleName) return moduleName
    if (line.id === defaultLineID) {
      return translate('lines.primaryLine')
    }
    const index = lines.findIndex(candidate => candidate === line)
    return translate('device.lineNumber', { number: index >= 0 ? index + 1 : 1 })
  }
  const identifier = lineID?.trim()
  return identifier
    ? translate('runtime.lineSuffix', { suffix: identifier.slice(-4) })
    : translate('lines.unknownLine')
}
