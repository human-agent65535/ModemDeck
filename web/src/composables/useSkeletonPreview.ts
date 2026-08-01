import { fixtureMode } from '../api/client'

function previewRequested(): boolean {
  if (!fixtureMode || typeof window === 'undefined') return false
  const query = new URLSearchParams(window.location.search)
  if (query.get('skeleton') === '1') return true
  return (query.get('ux') || '')
    .split(/[,+\s]/)
    .some(value => value.trim() === 'skeleton')
}

export const skeletonPreviewEnabled = previewRequested()
