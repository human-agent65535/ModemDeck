import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const messagesView = new URL('../src/views/MessagesView.vue', import.meta.url)

test('existing conversations default to their original line and expose the selector', async () => {
  const source = await readFile(messagesView, 'utf8')

  assert.match(source, /\[selectedThread, lines, composingNew\]/)
  assert.match(source, /thread\.line_id/)
  assert.match(source, /thread\.iccid/)
  assert.match(source, /const threadLine = lineForThread\(thread\)/)
  assert.match(
    source,
    /selectedLineKey\.value = threadLine \? lineKey\(threadLine\) : ''/
  )
  assert.match(
    source,
    /if \(replyLineThreadKey === thread\.key && selectedStillExists\) return/
  )
  assert.match(
    source,
    /<LineSelector[\s\S]*?v-if="lines\.length > 0"[\s\S]*?v-model="selectedLineKey"[\s\S]*?label="发送线路"/
  )
  assert.doesNotMatch(source, /v-if="composingNew && lines\.length > 0"/)
})

test('switching an existing conversation sends through the selected line', async () => {
  const source = await readFile(messagesView, 'utf8')

  assert.match(source, /const activeLine = computed\(\(\) => selectedLine\.value\)/)
  assert.match(source, /const activeICCID = computed\(\(\) => activeLine\.value\?\.iccid \|\| ''\)/)
  assert.match(
    source,
    /return threadUsesLine\(thread, line\) \? thread\.key : undefined/
  )
  assert.match(source, /thread_key: threadKey/)
  assert.match(source, /line_id: activeLineID\.value \|\| undefined/)
  assert.match(source, /iccid: activeICCID\.value \|\| undefined/)
  assert.match(source, /to: activeRecipient\.value/)
  assert.match(source, /const key = `\$\{sent\.iccid\}\|\$\{sent\.peer\}`/)
  assert.match(source, /if \(composingNew\.value \|\| key !== threadKey\)/)
  assert.match(
    source,
    /router\.replace\(\{ name: 'messages', params: \{ threadKey: key \} \}\)/
  )
  assert.doesNotMatch(source, /thread_key: selectedThread\.value\?\.key/)
  assert.doesNotMatch(source, /selectedThread\.value\?\.iccid \|\| ''/)
})

test('new messages retain contact preference then global default resolution', async () => {
  const source = await readFile(messagesView, 'utf8')

  assert.match(source, /resolveLine\('message', \{/)
  assert.match(source, /contextKey: composeContextLineKey\.value/)
  assert.match(source, /number: newRecipient\.value/)
  assert.match(source, /if \(!force && lineSelectionOverridden\) return/)
  assert.match(source, /if \(composingNew\.value\) lineSelectionOverridden = true/)
})
