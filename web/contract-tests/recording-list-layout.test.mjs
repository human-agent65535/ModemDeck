import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const recordingList = new URL(
  '../src/components/RecordingList.vue',
  import.meta.url
)

test('recording rows stay compact when wide and split by container width', async () => {
  const source = await readFile(recordingList, 'utf8')

  assert.match(source, /\.recording-list\s*\{[^}]*container-type: inline-size;/s)
  assert.match(
    source,
    /\.recording-list li\s*\{[^}]*grid-template-columns: minmax\(240px, 0\.9fr\) minmax\(220px, 1fr\) 34px 34px;/s
  )
  assert.match(
    source,
    /@container \(max-width: 720px\)[\s\S]*?\.recording-list__meta\s*\{[^}]*grid-column: 1;[^}]*grid-row: 1;/s
  )
  assert.match(
    source,
    /@container \(max-width: 720px\)[\s\S]*?\.recording-list__player\s*\{[^}]*grid-column: 1 \/ 4;[^}]*grid-row: 2;/s
  )
  assert.match(
    source,
    /@container \(max-width: 720px\)[\s\S]*?\.recording-list a\s*\{[^}]*grid-column: 2;[^}]*grid-row: 1;/s
  )
  assert.match(
    source,
    /@container \(max-width: 720px\)[\s\S]*?\.recording-list__delete\s*\{[^}]*grid-column: 3;[^}]*grid-row: 1;/s
  )
})
