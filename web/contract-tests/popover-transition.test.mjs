import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { h, nextTick } from 'vue'
import { i18n } from '../src/i18n/index.ts'
import { cssRules, declaration, splitTopLevel, styleBlocks } from './helpers/css-rules.mjs'
import { findNodes, hasClass, loadComponent, mountComponent } from './helpers/component-harness.mjs'

async function source(path) {
  return readFile(new URL(path, import.meta.url), 'utf8')
}

test('anchored menus and listboxes share one motion primitive', async () => {
  const [transition, audio, incoming, line, country, contactSuggest] = await Promise.all([
    source('../src/components/PopoverTransition.vue'),
    source('../src/components/AudioSettingsMenu.vue'),
    source('../src/components/IncomingCallModeControl.vue'),
    source('../src/components/LineSelector.vue'),
    source('../src/components/CountryRegionSelector.vue'),
    source('../src/components/ContactSuggestInput.vue')
  ])

  assert.match(transition, /<Transition name="popover-motion">/)
  assert.match(transition, /opacity var\(--motion-fast\)/)
  assert.match(transition, /transform var\(--motion-base\)/)
  assert.match(transition, /prefers-reduced-motion: reduce/)

  for (const consumer of [audio, incoming, line, country, contactSuggest]) {
    assert.match(consumer, /import PopoverTransition from/)
    assert.match(consumer, /<PopoverTransition>/)
  }
})

test('audio popover closes when keyboard focus leaves it', async () => {
  const audio = await source('../src/components/AudioSettingsMenu.vue')

  assert.match(audio, /function onDocumentFocusIn/)
  assert.match(audio, /document\.addEventListener\('focusin', onDocumentFocusIn\)/)
  assert.match(audio, /document\.removeEventListener\('focusin', onDocumentFocusIn\)/)
})

function motionMilliseconds(value, tokens) {
  const token = value.match(/var\((--[\w-]+)\)/)?.[1]
  const resolved = token ? tokens.get(token) : value
  const match = resolved?.match(/^([\d.]+)(ms|s)$/)
  assert.ok(match, `${value} resolves to a duration`)
  return Number(match[1]) * (match[2] === 's' ? 1000 : 1)
}

test('workspace action overflow fades out before it hides, never via display: none', async () => {
  const [actions, transition, styles] = await Promise.all([
    source('../src/components/workspace/WorkspaceDetailActions.vue'),
    source('../src/components/PopoverTransition.vue'),
    source('../src/style.css')
  ])
  const tokens = new Map(
    [...styles.matchAll(/^\s*(--motion-[\w-]+):\s*([^;]+);/gm)].map(match => [match[1], match[2].trim()])
  )

  if (/<PopoverTransition>/.test(actions)) {
    // The shared primitive owns the motion; its own contract is tested above.
    for (const rule of cssRules(styleBlocks(transition).join('\n'))) {
      assert.equal(declaration(rule, 'display'), undefined)
    }
    return
  }

  const menu = '.workspace-detail-actions__secondary'
  const rules = cssRules(styleBlocks(actions).join('\n')).filter(rule =>
    rule.selectors.some(selector => selector === menu || selector === `${menu}.is-open`)
  )
  // Closed and open differ only in visibility/opacity: no state removes the box.
  for (const rule of rules) assert.equal(declaration(rule, 'display') === 'none', false)

  // The cascade at full motion: later rules override earlier ones.
  const cascade = selectors => ({
    declarations: rules
      .filter(rule => rule.conditions.length === 0 && rule.selectors.some(s => selectors.includes(s)))
      .flatMap(rule => rule.declarations)
  })
  const closed = cascade([menu])
  const open = cascade([menu, `${menu}.is-open`])
  assert.equal(declaration(closed, 'visibility'), 'hidden')
  assert.equal(declaration(closed, 'opacity'), '0')
  assert.equal(declaration(closed, 'pointer-events'), 'none')
  assert.equal(declaration(open, 'visibility'), 'visible')
  assert.equal(declaration(open, 'opacity'), '1')

  // Closing: visibility flips only after the fade has finished. Opening: at once.
  const transitions = new Map(
    splitTopLevel(declaration(closed, 'transition'), ',').map(item => {
      const [property, ...timing] = splitTopLevel(item, ' ')
      // In a transition item the first time is the duration, the second the delay.
      const times = timing.filter(part => /^(var\(--motion-[\w-]+\)|[\d.]+m?s)$/.test(part))
      return [property, times.map(time => motionMilliseconds(time, tokens))]
    })
  )
  const [fade, fadeDelay = 0] = transitions.get('opacity')
  const [visibilityDuration, visibilityDelay = 0] = transitions.get('visibility')
  assert.equal(visibilityDuration, 0)
  assert.ok(visibilityDelay >= fade + fadeDelay, 'visibility waits for the fade')
  assert.equal(declaration(open, 'transition-delay'), '0s')
})

test('workspace action overflow closes on Escape, outside press, focus leaving and choosing an item', async t => {
  const document = new EventTarget()
  let listeners = 0
  const add = document.addEventListener.bind(document)
  const remove = document.removeEventListener.bind(document)
  document.addEventListener = (...args) => { listeners += 1; add(...args) }
  document.removeEventListener = (...args) => { listeners -= 1; remove(...args) }

  const WorkspaceDetailActions = await loadComponent(
    new URL('../src/components/workspace/WorkspaceDetailActions.vue', import.meta.url),
    { globals: { document } }
  )
  const { container, unmount } = mountComponent(WorkspaceDetailActions, {
    plugins: [i18n],
    slots: {
      primary: () => h('button', { class: 'primary-command' }),
      secondary: () => h('button', { class: 'delete-command' })
    }
  })
  let mounted = true
  t.after(() => { if (mounted) unmount() })

  const more = () => findNodes(container, node => node.props?.['aria-expanded'] !== undefined)[0]
  const item = () => findNodes(container, node => hasClass(node, 'delete-command'))[0]
  const outside = { parent: null }
  const isOpen = async () => {
    await nextTick()
    return more().props['aria-expanded'] === true
  }
  const openMenu = async () => {
    more().props.onClick(new Event('click'))
    assert.equal(await isOpen(), true)
  }
  const dispatch = (type, fields) => {
    const event = new Event(type, { cancelable: true })
    for (const [key, value] of Object.entries(fields)) {
      Object.defineProperty(event, key, { value })
    }
    document.dispatchEvent(event)
    return event
  }

  assert.equal(await isOpen(), false)

  await openMenu()
  dispatch('focusin', { target: item() })
  assert.equal(await isOpen(), true, 'focus moving inside the menu keeps it open')
  dispatch('pointerdown', { target: item() })
  assert.equal(await isOpen(), true, 'pressing inside the menu keeps it open')
  dispatch('focusin', { target: outside })
  assert.equal(await isOpen(), false, 'focus leaving closes it')

  await openMenu()
  const escape = dispatch('keydown', { key: 'Escape' })
  assert.equal(await isOpen(), false, 'Escape closes it')
  assert.equal(escape.defaultPrevented, true)
  const ignored = dispatch('keydown', { key: 'Escape' })
  assert.equal(ignored.defaultPrevented, false, 'Escape is left alone while closed')

  await openMenu()
  dispatch('pointerdown', { target: outside })
  assert.equal(await isOpen(), false, 'an outside press closes it')

  await openMenu()
  let menu = item()
  while (menu && !menu.props?.onClick) menu = menu.parent
  menu.props.onClick(new Event('click'))
  assert.equal(await isOpen(), false, 'choosing an item closes it')

  assert.ok(listeners > 0)
  unmount()
  mounted = false
  assert.equal(listeners, 0, 'document listeners are removed on unmount')
})
