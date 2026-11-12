/**
 * The Bot to show when the unsaved draft goes away: the one the person chose,
 * otherwise the one they were on before starting the draft if it still exists,
 * otherwise the first Bot, otherwise none.
 */
export function telegramDraftReturnID(
  units: ReadonlyArray<{ id: string }>,
  draftReturnID: string,
  nextID?: string
): string {
  if (nextID) return nextID
  if (draftReturnID && units.some(unit => unit.id === draftReturnID)) return draftReturnID
  return units[0]?.id || ''
}
