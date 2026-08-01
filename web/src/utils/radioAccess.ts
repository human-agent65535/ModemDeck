import type { ServingRadio } from '../api/types'

const ACCESS_TECHNOLOGIES: Array<{ mask: number; label: string }> = [
  { mask: 1 << 15, label: '5G NR' },
  { mask: 1 << 14, label: 'LTE' },
  { mask: 1 << 16, label: 'LTE-M' },
  { mask: 1 << 17, label: 'NB-IoT' },
  { mask: 1 << 9, label: 'HSPA+' },
  { mask: 1 << 8, label: 'HSPA' },
  { mask: 1 << 7, label: 'HSUPA' },
  { mask: 1 << 6, label: 'HSDPA' },
  { mask: 1 << 5, label: 'UMTS' },
  { mask: 1 << 4, label: 'EDGE' },
  { mask: 1 << 3, label: 'GPRS' },
  { mask: 1 << 2, label: 'GSM Compact' },
  { mask: 1 << 1, label: 'GSM' },
  { mask: 1 << 13, label: 'EVDO-B' },
  { mask: 1 << 12, label: 'EVDO-A' },
  { mask: 1 << 11, label: 'EVDO-0' },
  { mask: 1 << 10, label: '1xRTT' },
  { mask: 1, label: 'POTS' }
]

export function accessTechnologyLabel(value?: number | null): string {
  if (value == null || value === 0) return ''
  const unsigned = value >>> 0
  const labels = ACCESS_TECHNOLOGIES.filter(item => (unsigned & item.mask) !== 0).map(
    item => item.label
  )
  const knownMask = ACCESS_TECHNOLOGIES.reduce(
    (mask, item) => (mask | item.mask) >>> 0,
    0
  )
  const unknownMask = (unsigned & ~knownMask) >>> 0
  if (unknownMask !== 0) labels.push(`0x${unknownMask.toString(16).toUpperCase()}`)
  return labels.join(' / ') || `0x${unsigned.toString(16).toUpperCase()}`
}

export function servingTechnologyLabel(radio?: ServingRadio): string {
  switch (radio?.access_technology.trim().toLocaleLowerCase()) {
    case 'nr5g':
    case '5g':
      return '5G NR'
    case 'lte':
      return 'LTE'
    case 'umts':
      return 'UMTS'
    case 'gsm':
      return 'GSM'
    default:
      return radio?.access_technology.trim().toLocaleUpperCase() || ''
  }
}

export function detailedServingTechnologyLabel(
  radio?: ServingRadio,
  accessTechnologies?: number | null
): string {
  const technology = servingTechnologyLabel(radio) || accessTechnologyLabel(accessTechnologies)
  const duplex = radio?.duplex_mode?.trim().toLocaleUpperCase() || ''
  return [duplex, technology].filter(Boolean).join(' ')
}

export function servingBandLabel(radio?: ServingRadio): string {
  const band = radio?.band?.trim() || ''
  if (!band) return ''
  const technology = radio?.access_technology.trim().toLocaleLowerCase()
  const lteMatch = band.match(/^B(\d+)$/i)
  if (technology === 'lte' && lteMatch) return `LTE Band ${lteMatch[1]}`
  const nrMatch = band.match(/^n(\d+)$/i)
  if ((technology === 'nr5g' || technology === '5g') && nrMatch) {
    return `NR Band n${nrMatch[1]}`
  }
  return band
}

export function servingChannelLabel(radio?: ServingRadio): string {
  if (radio?.channel === undefined) return ''
  const channelType = radio.channel_type?.trim().toLocaleUpperCase() || 'Channel'
  return `${channelType} ${radio.channel}`
}
