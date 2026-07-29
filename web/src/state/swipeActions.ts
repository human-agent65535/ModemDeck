let closeActiveRow: (() => void) | undefined

export function activateSwipeRow(close: () => void): void {
  if (closeActiveRow !== close) closeActiveRow?.()
  closeActiveRow = close
}

export function clearSwipeRow(close: () => void): void {
  if (closeActiveRow === close) closeActiveRow = undefined
}
