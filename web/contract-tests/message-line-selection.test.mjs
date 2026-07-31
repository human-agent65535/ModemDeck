import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const messagesView = new URL('../src/views/MessagesView.vue', import.meta.url)
const workspace = new URL('../src/state/workspace.ts', import.meta.url)

test('existing conversations stay pinned to their original line', async () => {
  const source = await readFile(messagesView, 'utf8')

  assert.match(source, /lineKey\(line\) === thread\.line_id/)
  assert.doesNotMatch(source, /thread\.(?:local_phone|imsi|iccid)/)
  assert.match(
    source,
    /const activeLine = computed\(\(\) =>[\s\S]*composingNew\.value[\s\S]*selectedLine\.value[\s\S]*activeLineForThread\(selectedThread\.value\)/
  )
  assert.match(source, /return lineForKey\(thread\.line_id\)/)
  assert.doesNotMatch(source, /replyLineThreadKey/)
  assert.match(
    source,
    /<template v-if="composingNew">[\s\S]*?<LineSelector[\s\S]*?v-model="selectedLineKey"[\s\S]*?compact/
  )
  assert.doesNotMatch(
    source,
    /<footer class="message-composer">[\s\S]*?<LineSelector/
  )
})

test('sending an existing conversation uses its original line identity', async () => {
  const source = await readFile(messagesView, 'utf8')

  assert.match(
    source,
    /const activeLine = computed\(\(\) =>[\s\S]*activeLineForThread\(selectedThread\.value\)/
  )
  assert.match(
    source,
    /return threadUsesLine\(thread, line\) \? thread\.key : undefined/
  )
  assert.match(source, /thread_key: replyKey/)
  assert.match(source, /const replyKey = replyThreadKey\.value/)
  assert.match(source, /line_id: activeLineID\.value/)
  assert.doesNotMatch(source, /activeICCID|iccid:/)
  assert.match(source, /to: activeRecipient\.value/)
  assert.match(source, /const sentThread = result\.thread/)
  assert.match(source, /sentThread\.key !== replyKey/)
  assert.match(
    source,
    /router\.replace\(\{[\s\S]*name: 'messages',[\s\S]*params: \{ threadKey: sentThread\.key \},[\s\S]*query: messageFilterQuery\(\)[\s\S]*\}\)/
  )
  assert.doesNotMatch(source, /`\$\{sent\.iccid\}\|\$\{sent\.peer\}`/)
  assert.doesNotMatch(source, /thread_key: selectedThread\.value\?\.key/)
})

test('message loads, reads, and post-send reconciliation use backend line identity', async () => {
  const source = await readFile(workspace, 'utf8')

  assert.match(source, /function messageQueryForThread\(thread: MessageThread\)/)
  assert.match(source, /line_id: thread\.line_id/)
  assert.match(source, /peer: thread\.peer/)
  assert.match(source, /gateway\.listMessages\(messageQueryForThread\(thread\)\)/)
  assert.match(source, /requestThreadRead\(messageQueryForThread\(thread\)\)/)
  assert.match(source, /const threads = await refreshThreads\(\)/)
  assert.match(source, /thread\.line_id === lineID/)
  assert.match(
    source,
    /normalizedAddress\(thread\.peer\) === normalizedAddress\(sent\.peer\)/
  )
  assert.doesNotMatch(source, /thread\.(?:local_phone|imsi|iccid)/)
  assert.doesNotMatch(source, /const key = `\$\{sent\.iccid\}\|\$\{sent\.peer\}`/)
})

test('new messages retain contact preference then global default resolution', async () => {
  const source = await readFile(messagesView, 'utf8')

  assert.match(source, /resolveLine\('message', \{/)
  assert.match(source, /contextKey: composeContextLineKey\.value/)
  assert.match(source, /preferredLineID: selectedRecipientPreferredLineID\.value/)
  assert.match(source, /number: newRecipient\.value/)
  assert.match(source, /if \(!force && lineSelectionOverridden\) return/)
  assert.match(source, /if \(composingNew\.value\) lineSelectionOverridden = true/)
  assert.match(source, /function chooseRecipient[\s\S]*?syncComposeLine\(\)/)
  assert.doesNotMatch(source, /function chooseRecipient[\s\S]*?syncComposeLine\(true\)/)
  assert.match(source, /findRecipientThread\(/)
  assert.match(source, /messageReturnRoute\(composeReturnThreadKey\)/)
  assert.match(
    source,
    /<template v-if="composingNew" #leading>[\s\S]*class="icon-button mobile-compose-cancel"[\s\S]*t\('messages\.cancelNew'\)/
  )
  assert.doesNotMatch(source, /'mobile-back'|t\('messages\.back'\)/)
})
