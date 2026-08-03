import assert from 'node:assert/strict'
import { readdir, readFile } from 'node:fs/promises'
import test from 'node:test'

const sourceRoot = new URL('../src/', import.meta.url)

async function vueFiles(directory = sourceRoot) {
  const entries = await readdir(directory, { withFileTypes: true })
  const nested = await Promise.all(
    entries.map(async entry => {
      if (entry.isDirectory()) {
        return vueFiles(new URL(`${entry.name}/`, directory))
      }
      return entry.name.endsWith('.vue') ? [new URL(entry.name, directory)] : []
    })
  )
  return nested.flat()
}

test('visible form choices and ranges use shared branded primitives', async () => {
  const files = await vueFiles()

  for (const file of files) {
    const contents = await readFile(file, 'utf8')
    assert.doesNotMatch(contents, /<select\b/, `${file.pathname} exposes a native select`)

    for (const match of contents.matchAll(/<input\b[\s\S]*?>/g)) {
      const input = match[0]
      const type = input.match(/\btype="([^"]+)"/)?.[1]
      if (type === 'range') {
        assert.match(
          input,
          /\bclass="[^"]*\bui-range\b[^"]*"/,
          `${file.pathname} exposes an unbranded range input`
        )
      }
      if (type === 'checkbox' || type === 'radio') {
        assert.match(
          input,
          /\bclass="[^"]*\bui-(?:switch|check|radio|choice-input--hidden)\b[^"]*"/,
          `${file.pathname} exposes an unbranded ${type}`
        )
      }
    }

    for (const match of contents.matchAll(/<summary\b[\s\S]*?>/g)) {
      assert.match(
        match[0],
        /\bclass="[^"]*\bui-disclosure-summary\b[^"]*"/,
        `${file.pathname} exposes a browser-native disclosure marker`
      )
    }
  }
})

test('shared control CSS removes remaining browser decorations', async () => {
  const style = await readFile(new URL('../src/style.css', import.meta.url), 'utf8')

  for (const primitive of [
    'ui-range',
    'ui-check',
    'ui-radio',
    'ui-choice-input--hidden',
    'ui-disclosure-summary'
  ]) {
    assert.match(style, new RegExp(`\\.${primitive}\\s*\\{`), `missing .${primitive}`)
  }

  assert.match(style, /input\[type="search"\]::\-webkit-search-cancel-button/)
  assert.match(style, /input\[type="number"\]::\-webkit-inner-spin-button/)
  assert.match(style, /\.ui-range::\-webkit-slider-thumb/)
  assert.match(style, /\.ui-range::\-moz-range-thumb/)
})
