export const DISCLAIMER_INTERVAL_MS = 7 * 24 * 60 * 60 * 1000

export function shouldShowDisclaimer(lastAgreedAt: number | null, now: number): boolean {
  if (lastAgreedAt === null || Number.isNaN(lastAgreedAt)) {
    return true
  }
  return now - lastAgreedAt >= DISCLAIMER_INTERVAL_MS
}

export function shouldRequireDisclaimerConfirmationText(lastAgreedAt: number | null): boolean {
  return lastAgreedAt === null || Number.isNaN(lastAgreedAt)
}
