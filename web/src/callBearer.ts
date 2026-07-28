export function knownCallBearerLabel(value: string | undefined): string {
  switch (value?.trim().toLocaleLowerCase()) {
    case 'volte':
      return 'VoLTE'
    case 'vowifi':
      return 'VoWiFi'
    case 'gsm':
    case 'cs':
    case 'circuit-switched':
      return 'GSM / CS'
    default:
      return ''
  }
}
