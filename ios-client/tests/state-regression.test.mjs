import assert from 'node:assert/strict'
import { readFile, mkdtemp, writeFile, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { spawnSync } from 'node:child_process'
import test from 'node:test'

function declaration(source, marker) {
  const start = source.indexOf(marker)
  assert.ok(start >= 0, `missing ${marker}`)
  const body = source.indexOf('{', start)
  let depth = 0
  for (let index = body; index < source.length; index++) {
    if (source[index] === '{') depth++
    if (source[index] === '}' && --depth === 0) return source.slice(start, index + 1)
  }
  assert.fail(`unclosed ${marker}`)
}

async function verify(template, declarations) {
  const directory = await mkdtemp(path.join(tmpdir(), 'modemdeck-swift-regression-'))
  try {
    const stubs = await readFile(new URL(`fixtures/${template}`, import.meta.url), 'utf8')
    const file = path.join(directory, 'test.swift')
    const binary = path.join(directory, 'test')
    await writeFile(file, stubs.replace('// INSERT_PRODUCT_METHODS', declarations.join('\n')))
    const compiled = spawnSync('xcrun', ['swiftc', '-parse-as-library', file, '-o', binary], {
      env: { ...process.env, DEVELOPER_DIR: '/Applications/Xcode-beta.app/Contents/Developer' }, encoding: 'utf8'
    })
    assert.equal(compiled.status, 0, compiled.stderr)
    const result = spawnSync(binary, [], { encoding: 'utf8' })
    assert.equal(result.status, 0, result.stderr)
  } finally { await rm(directory, { recursive: true, force: true }) }
}

test('native collections fetch all pages, retain filters, and reject partial snapshots', { skip: process.platform !== 'darwin' }, async () => {
  const source = await readFile(new URL('../ios/App/App/ModemDeckAPI.swift', import.meta.url), 'utf8')
  await verify('pagination-stubs.swift', ['func contacts(', 'func calls()', 'func recordings()',
    'private func allPages<', 'private func path('].map(marker => declaration(source, marker)))
})

test('native conversation refresh removes deleted cache entries and send completion preserves newer drafts', { skip: process.platform !== 'darwin' }, async () => {
  const source = await readFile(new URL('../ios/App/App/ModemDeckSession.swift', import.meta.url), 'utf8')
  await verify('conversation-stubs.swift', ['final class ModemDeckConversationStore:', 'final class ModemDeckMessageDraft:']
    .map(marker => '@MainActor\n' + declaration(source, marker)))
})
