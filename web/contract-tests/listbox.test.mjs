import assert from 'node:assert/strict'
import test from 'node:test'
import { nextTick, ref } from 'vue'
import { useListbox } from '../src/composables/useListbox.ts'

function key(name) {
  return {
    key: name,
    prevented: false,
    stopped: false,
    preventDefault() { this.prevented = true },
    stopPropagation() { this.stopped = true }
  }
}

function listbox(options, { selected = -1, disabled = false } = {}) {
  const chosen = []
  const state = { disabled, options }
  const box = useListbox({
    root: ref(undefined),
    trigger: ref(undefined),
    options: () => state.options,
    disabled: () => state.disabled,
    selectedIndex: () => selected,
    optionSelector: index => `[data-index="${index}"]`,
    select: option => chosen.push(option.id)
  })
  return { box, chosen, state }
}

const lines = [
  { id: 'a', disabled: true },
  { id: 'b' },
  { id: 'c', disabled: true },
  { id: 'd' },
  { id: 'e', disabled: true }
]

test('a listbox opens on the selected option, or the first one that can be chosen', () => {
  const none = listbox(lines)
  none.box.openMenu()
  assert.equal(none.box.open.value, true)
  assert.equal(none.box.activeIndex.value, 1)

  const selected = listbox(lines, { selected: 3 })
  selected.box.openMenu()
  assert.equal(selected.box.activeIndex.value, 3)

  const first = listbox(lines, { selected: 3 })
  first.box.onTriggerKeydown(key('ArrowDown'))
  assert.equal(first.box.activeIndex.value, 1)
  const last = listbox(lines, { selected: 1 })
  last.box.onTriggerKeydown(key('ArrowUp'))
  assert.equal(last.box.activeIndex.value, 3)
})

test('arrow keys, Home and End never land on a disabled option', () => {
  const { box } = listbox(lines, { selected: 1 })
  box.openMenu()
  box.onOptionKeydown(key('ArrowDown'), 1)
  assert.equal(box.activeIndex.value, 3)
  box.onOptionKeydown(key('ArrowDown'), 3)
  assert.equal(box.activeIndex.value, 1, 'wraps past the disabled last and first options')
  box.onOptionKeydown(key('ArrowUp'), 1)
  assert.equal(box.activeIndex.value, 3)
  box.onOptionKeydown(key('Home'), 3)
  assert.equal(box.activeIndex.value, 1)
  box.onOptionKeydown(key('End'), 1)
  assert.equal(box.activeIndex.value, 3)
})

test('Enter and Space choose an option that can be chosen and nothing else', () => {
  const { box, chosen } = listbox(lines, { selected: 1 })
  box.openMenu()
  box.onOptionKeydown(key('Enter'), 3)
  box.onOptionKeydown(key(' '), 1)
  box.onOptionKeydown(key('Enter'), 0)
  assert.deepEqual(chosen, ['d', 'b'])
})

test('Escape closes an open listbox and is left alone when it is closed', () => {
  const { box } = listbox(lines)
  const closed = key('Escape')
  box.onTriggerKeydown(closed)
  assert.equal(closed.stopped, false, 'a closed listbox lets a surrounding dialog handle Escape')

  box.openMenu()
  const opened = key('Escape')
  box.onTriggerKeydown(opened)
  assert.equal(box.open.value, false)
  assert.equal(opened.stopped && opened.prevented, true)

  box.openMenu()
  box.onOptionKeydown(key('Tab'), 1)
  assert.equal(box.open.value, false)
})

test('a disabled or empty listbox does not open, and disabling closes it', async () => {
  assert.equal(listbox([]).box.open.value, false)
  const empty = listbox([])
  empty.box.openMenu()
  assert.equal(empty.box.open.value, false)

  const off = listbox(lines, { disabled: true })
  off.box.toggleMenu()
  assert.equal(off.box.open.value, false)

  const disabledState = ref(false)
  const box = useListbox({
    root: ref(undefined), trigger: ref(undefined), options: () => lines,
    disabled: () => disabledState.value, selectedIndex: () => -1,
    optionSelector: index => `[data-index="${index}"]`, select() {}
  })
  box.openMenu()
  disabledState.value = true
  await nextTick()
  assert.equal(box.open.value, false)
})
