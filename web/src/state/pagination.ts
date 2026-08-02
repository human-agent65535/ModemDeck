import type { PageMeta } from '../api/types'

export type PaginationState = {
  nextCursor: string
  hasMore: boolean
  loadingMore: boolean
  error: string
  pages: number
  generation: number
}

export function paginationState(): PaginationState {
  return {
    nextCursor: '',
    hasMore: false,
    loadingMore: false,
    error: '',
    pages: 0,
    generation: 0
  }
}

export function resetPagination(state: PaginationState): void {
  state.generation += 1
  state.nextCursor = ''
  state.hasMore = false
  state.loadingMore = false
  state.error = ''
  state.pages = 0
}

export function acceptFirstPage(state: PaginationState, meta: PageMeta): void {
  state.nextCursor = meta.next_cursor
  state.hasMore = meta.has_more
  state.loadingMore = false
  state.error = ''
  state.pages = 1
}

export function acceptNextPage(state: PaginationState, meta: PageMeta): void {
  state.nextCursor = meta.next_cursor
  state.hasMore = meta.has_more
  state.loadingMore = false
  state.error = ''
  state.pages += 1
}

export function replaceFirstPage<T>(
  state: PaginationState,
  items: T[],
  meta: PageMeta
): T[] {
  resetPagination(state)
  acceptFirstPage(state, meta)
  return items
}

export function mergeUnique<T>(
  first: T[],
  second: T[],
  identity: (item: T) => string
): T[] {
  const merged = new Map<string, T>()
  for (const item of first) merged.set(identity(item), item)
  for (const item of second) {
    const key = identity(item)
    if (!merged.has(key)) merged.set(key, item)
  }
  return Array.from(merged.values())
}
