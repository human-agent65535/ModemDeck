import type { LineSummary } from '../api/types'

export type LineTagLine = Pick<LineSummary, 'id' | 'iccid' | 'line_label'>

export function createLineLookup(lines: LineSummary[]): Map<string, LineSummary> {
  const lookup = new Map<string, LineSummary>()
  for (const line of lines) {
    for (const identifier of [line.id, line.iccid, line.device_imei]) {
      if (identifier) lookup.set(identifier, line)
    }
  }
  return lookup
}

export function findLine(
  lookup: ReadonlyMap<string, LineSummary>,
  ...identifiers: Array<string | undefined>
): LineSummary | undefined {
  for (const identifier of identifiers) {
    const normalized = identifier?.trim()
    if (!normalized) continue
    const line = lookup.get(normalized)
    if (line) return line
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
    if (line.device_imei && line.device_imei === defaultDeviceIMEI) return '主卡'
    const index = lines.findIndex(candidate => candidate === line)
    return `线路 ${index >= 0 ? index + 1 : 1}`
  }
  const identifier = identifiers.find(value => value?.trim())?.trim()
  return identifier ? `线路 ${identifier.slice(-4)}` : '未知线路'
}
