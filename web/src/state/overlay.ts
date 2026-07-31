const overlayStack: string[] = []

function syncApplicationInert(): void {
  if (typeof document === 'undefined') return
  const application = document.querySelector<HTMLElement>('#app')
  if (!application) return
  application.inert = overlayStack.length > 0
}

export function registerOverlay(id: string): void {
  const existing = overlayStack.indexOf(id)
  if (existing >= 0) overlayStack.splice(existing, 1)
  overlayStack.push(id)
  syncApplicationInert()
}

export function unregisterOverlay(id: string): void {
  const existing = overlayStack.indexOf(id)
  if (existing >= 0) overlayStack.splice(existing, 1)
  syncApplicationInert()
}

export function overlayIsTopmost(id: string): boolean {
  return overlayStack.at(-1) === id
}
