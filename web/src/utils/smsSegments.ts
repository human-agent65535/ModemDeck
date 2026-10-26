// GSM 03.38 default alphabet and its escape-table extension (each extension character costs two septets).
const GSM_BASIC =
  '@£$¥èéùìòÇ\nØø\rÅåΔ_ΦΓΛΩΠΨΣΘΞÆæßÉ !"#¤%&\'()*+,-./0123456789:;<=>?' +
  '¡ABCDEFGHIJKLMNOPQRSTUVWXYZÄÖÑÜ§¿abcdefghijklmnopqrstuvwxyzäöñüà'
const GSM_EXTENDED = '^{}\\[~]|€\f'

export type SMSLength = {
  encoding: 'gsm7' | 'ucs2'
  /** Units used: septets for GSM-7, UTF-16 code units for UCS-2. */
  units: number
  segments: number
  /** Units available in the current number of segments. */
  limit: number
}

/** How a draft will be split into SMS parts, so the sender can see when a reply costs more than one. */
export function smsLength(text: string): SMSLength {
  let septets = 0
  let gsm = true
  for (const character of text) {
    if (GSM_BASIC.includes(character)) septets += 1
    else if (GSM_EXTENDED.includes(character)) septets += 2
    else {
      gsm = false
      break
    }
  }
  const encoding = gsm ? 'gsm7' : 'ucs2'
  const units = gsm ? septets : text.length
  const single = gsm ? 160 : 70
  const part = gsm ? 153 : 67
  const segments = units === 0 ? 0 : units <= single ? 1 : Math.ceil(units / part)
  const limit = segments <= 1 ? single : segments * part
  return { encoding, units, segments, limit }
}
