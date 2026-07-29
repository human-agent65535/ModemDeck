import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

async function source(path) {
  return readFile(new URL(path, import.meta.url), 'utf8')
}

test('line filters use the shared compact selector on desktop and mobile', async () => {
  const selector = await source('../src/components/LineSelector.vue')
  const calls = await source('../src/views/CallsView.vue')
  const messages = await source('../src/views/MessagesView.vue')
  const recordings = await source('../src/views/RecordingsView.vue')
  const styles = await source('../src/style.css')

  assert.match(selector, /filterMode\?: boolean/)
  assert.match(selector, /'is-filter': filterMode/)
  assert.match(selector, /<ListFilter v-if="isAllSelected"/)
  assert.match(selector, /<CardSim v-else/)
  assert.match(selector, /<CardSim v-if="option\.line"/)
  assert.match(selector, /:style="toneStyle\(option\.line\)"/)
  assert.doesNotMatch(selector, /RadioTower/)
  assert.match(selector, /\.line-selector\.is-filter \{/)
  assert.match(
    styles,
    /\.pane-search-row > \.line-selector\.is-filter \{[\s\S]*position: absolute;/
  )

  for (const view of [calls, messages, recordings]) {
    assert.match(view, /class="pane-search-row"/)
    assert.match(view, /include-all[\s\S]*filter-mode/)
  }
})

test('existing message threads keep their original line', async () => {
  const messages = await source('../src/views/MessagesView.vue')
  const english = await source('../src/i18n/locales/en-US.ts')
  const chinese = await source('../src/i18n/locales/zh-CN.ts')

  assert.match(
    messages,
    /class="conversation-recipient"[\s\S]*class="compose-line-select"[\s\S]*compact/
  )
  assert.doesNotMatch(messages, /conversation-recipient__label/)
  assert.doesNotMatch(
    messages,
    /<footer class="message-composer">[\s\S]*<LineSelector/
  )
  assert.match(
    messages,
    /v-if="selectedThread && !composingNew && activeRecipientIsContactable"[\s\S]*:title="dialUnavailable \|\| t\('calls\.dial'\)"/
  )
  assert.match(english, /search: 'Search messages, names, or numbers'/)
  assert.match(chinese, /search: '搜索短信、姓名或号码'/)
})
