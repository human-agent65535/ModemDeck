import type { LineSummary } from '../api/types'
import { translate } from '../i18n'

export type LineTagLine = Pick<LineSummary, 'id' | 'iccid' | 'line_label' | 'line_color'>

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

function lookupKeys(value: string | undefined): string[] {
  const normalized = value?.trim()
  if (!normalized) return []
  const digits = normalizedPhoneIdentity(normalized)
  return digits && digits !== normalized ? [normalized, digits] : [normalized]
}

export function createLineLookup(lines: LineSummary[]): Map<string, LineSummary> {
  const lookup = new Map<string, LineSummary>()
  for (const line of lines) {
    for (const identifier of [
      line.id,
      line.phone_number,
      line.imsi,
      line.iccid,
      line.device_imei
    ]) {
      for (const key of lookupKeys(identifier)) lookup.set(key, line)
    }
  }
  return lookup
}

export function findLine(
  lookup: ReadonlyMap<string, LineSummary>,
  ...identifiers: Array<string | undefined>
): LineSummary | undefined {
  for (const identifier of identifiers) {
    for (const key of lookupKeys(identifier)) {
      const line = lookup.get(key)
      if (line) return line
    }
  }
  return undefined
}

export function lineTagLine(
  line: LineSummary | undefined,
  ...identifiers: Array<string | undefined>
): LineTagLine {
  if (line) return line
  return {
    id: identifiers.find(identifier => identifier?.trim())?.trim(),
    iccid: '',
    line_label: ''
  }
}

export function lineTagFallback(
  line: LineSummary | undefined,
  lines: LineSummary[],
  defaultDeviceIMEI: string,
  ...identifiers: Array<string | undefined>
): string {
  if (line) {
    const moduleName = line.device_alias.trim() || line.model?.trim()
    if (moduleName) return moduleName
    if (line.device_imei && line.device_imei === defaultDeviceIMEI) {
      return translate('lines.primaryLine')
    }
    const index = lines.findIndex(candidate => candidate === line)
    return translate('device.lineNumber', { number: index >= 0 ? index + 1 : 1 })
  }
  const identifier = identifiers.find(value => value?.trim())?.trim()
  return identifier
    ? translate('runtime.lineSuffix', { suffix: identifier.slice(-4) })
    : translate('lines.unknownLine')
}
