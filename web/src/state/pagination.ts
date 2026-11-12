import type { Page, PageMeta, ResourceStatus } from '../api/types'
import { ApiError } from '../api/types'

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

export type NextPageOptions<T> = {
  target: { status: ResourceStatus; data: T[] }
  pagination: PaginationState
  // The owner's generation counter; a change while the page loads discards it.
  generation: () => number
  loader: (cursor: string) => Promise<Page<T>>
  // Called when the server rejects the cursor; the owner reloads its first page.
  restart: () => Promise<T[] | null>
  identity: (item: T) => string
  errorMessage: (error: unknown) => string
  prepend?: boolean
}

export async function loadNextPage<T>(options: NextPageOptions<T>): Promise<T[] | null> {
  const { target, pagination } = options
  const cursor = pagination.nextCursor
  if (
    target.status !== 'ready' ||
    !pagination.hasMore ||
    !cursor ||
    pagination.loadingMore
  ) return target.data

  const generation = options.generation()
  const pageGeneration = pagination.generation
  pagination.loadingMore = true
  pagination.error = ''
  try {
    const page = await options.loader(cursor)
    if (
      generation !== options.generation() ||
      pageGeneration !== pagination.generation ||
      pagination.nextCursor !== cursor
    ) return null
    target.data = options.prepend
      ? mergeUnique(page.items, target.data, options.identity)
      : mergeUnique(target.data, page.items, options.identity)
    acceptNextPage(pagination, page.meta)
    return target.data
  } catch (error) {
    if (
      generation !== options.generation() ||
      pageGeneration !== pagination.generation
    ) return null
    if (
      error instanceof ApiError &&
      error.code === 'invalid_argument' &&
      error.field === 'cursor'
    ) {
      return options.restart()
    }
    pagination.error = options.errorMessage(error)
    return null
  } finally {
    if (
      generation === options.generation() &&
      pageGeneration === pagination.generation
    ) {
      pagination.loadingMore = false
    }
  }
}
