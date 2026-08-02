import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

import {
  acceptFirstPage,
  acceptNextPage,
  paginationState,
  replaceFirstPage
} from '../src/state/pagination.ts'

test('an authoritative first page replaces loaded pages and restarts backend pagination', () => {
  const pagination = paginationState()
  acceptFirstPage(pagination, {
    limit: 1,
    next_cursor: 'old-page-2',
    has_more: true
  })
  acceptNextPage(pagination, {
    limit: 1,
    next_cursor: 'old-page-3',
    has_more: true
  })

  const items = replaceFirstPage(pagination, ['a'], {
    limit: 1,
    next_cursor: 'new-page-2',
    has_more: true
  })

  assert.deepEqual(items, ['a'])
  assert.equal(pagination.pages, 1)
  assert.equal(pagination.nextCursor, 'new-page-2')
  assert.equal(pagination.hasMore, true)
  assert.equal(pagination.loadingMore, false)
  assert.equal(pagination.error, '')
})

test('durable list owners replace the first page while later pages use backend cursors', async () => {
  const [workspace, recording] = await Promise.all([
    readFile(new URL('../src/state/workspace.ts', import.meta.url), 'utf8'),
    readFile(new URL('../src/state/recording.ts', import.meta.url), 'utf8')
  ])

  assert.match(
    workspace,
    /target\.data = replaceFirstPage\(pagination, page\.items, page\.meta\)/
  )
  assert.match(workspace, /cursor => gateway\.listContacts\(\{ cursor \}\)/)
  assert.match(workspace, /cursor => gateway\.listThreads\(\{ cursor \}\)/)
  assert.match(
    workspace,
    /cursor => gateway\.listCalls\(callsQueryFilter, \{ cursor \}\)/
  )
  assert.match(
    workspace,
    /cursor => gateway\.listMessages\(\{[\s\S]*?cursor[\s\S]*?\}\)/
  )
  assert.match(
    recording,
    /recordingCatalogState\.data = replaceFirstPage\([\s\S]*?page\.items,[\s\S]*?page\.meta[\s\S]*?\)/
  )
  assert.match(
    recording,
    /gateway\.listRecordings\(\{[\s\S]*?cursor[\s\S]*?\}\)/
  )
})
