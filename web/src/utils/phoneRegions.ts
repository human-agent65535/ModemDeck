import {
  getCountries,
  getCountryCallingCode,
  type CountryCode
} from 'libphonenumber-js/min'

export type PhoneRegionOption = {
  region: CountryCode
  name: string
  callingCode: string
}

function normalizedRegions(regions: string[]): CountryCode[] {
  const supported = new Set(getCountries())
  const normalized: CountryCode[] = []
  for (const value of regions) {
    const region = value.trim().toUpperCase() as CountryCode
    if (supported.has(region) && !normalized.includes(region)) normalized.push(region)
  }
  return normalized
}

export function phoneRegionOptions(
  locale: string,
  preferredRegions: string[] = []
): PhoneRegionOption[] {
  const preferred = normalizedRegions(preferredRegions)
  const preferredIndex = new Map(preferred.map((region, index) => [region, index]))
  const names = new Intl.DisplayNames([locale || 'en'], { type: 'region' })
  const collator = new Intl.Collator(locale || 'en', { sensitivity: 'base' })

  return getCountries()
    .map(region => ({
      region,
      name: names.of(region) || region,
      callingCode: getCountryCallingCode(region)
    }))
    .sort((left, right) => {
      const leftPreferred = preferredIndex.get(left.region)
      const rightPreferred = preferredIndex.get(right.region)
      if (leftPreferred !== undefined || rightPreferred !== undefined) {
        if (leftPreferred === undefined) return 1
        if (rightPreferred === undefined) return -1
        return leftPreferred - rightPreferred
      }
      return collator.compare(left.name, right.name)
    })
}

export function filterPhoneRegionOptions(
  options: PhoneRegionOption[],
  query: string
): PhoneRegionOption[] {
  const normalized = query.trim().toLocaleLowerCase()
  if (!normalized) return options
  const callingCode = normalized.replace(/^\+/, '')

  return options.filter(option =>
    option.region.toLocaleLowerCase().includes(normalized) ||
    option.name.toLocaleLowerCase().includes(normalized) ||
    option.callingCode.startsWith(callingCode)
  )
}
