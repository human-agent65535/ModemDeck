import type { LineSummary } from '../api/types'
import { translate } from '../i18n'

export type LineTagLine = Pick<LineSummary, 'id' | 'line_label' | 'line_color'>

export function normalizedPhoneIdentity(value: string | undefined): string {
  const source = value?.trim() || ''
  const digits = source.replace(/\D/g, '')
  if (source.startsWith('00')) {
    const internationalDigits = digits.slice(2)
    if (/^[1-9]\d{7,14}$/.test(internationalDigits)) return internationalDigits
  }
  return digits
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
