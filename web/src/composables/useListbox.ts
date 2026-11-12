import {
  getCurrentInstance,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  watch,
  type Ref
} from 'vue'

type ListboxOption = { disabled?: boolean }

/**
 * The behaviour of a select-style listbox popover: opening and closing, which
 * option is active, keyboard movement, and closing when focus or a press goes
 * elsewhere. The component renders its own trigger and options.
 *
 * A disabled option is never made active: arrow keys, Home and End skip it, in
 * every listbox that uses this.
 */
export function useListbox<Option extends ListboxOption>(config: {
  root: Ref<HTMLElement | undefined>
  trigger: Ref<HTMLElement | undefined>
  options: () => readonly Option[]
  disabled: () => boolean
  /** Index of the option that is selected now, or -1. */
  selectedIndex: () => number
  /** CSS selector, inside `root`, of the option element at `index`. */
  optionSelector: (index: number) => string
  select: (option: Option) => void
}) {
  const open = ref(false)
  const activeIndex = ref(0)

  function firstEnabledIndex(): number {
    const index = config.options().findIndex(option => !option.disabled)
    return index >= 0 ? index : 0
  }

  function lastEnabledIndex(): number {
    const options = config.options()
    for (let index = options.length - 1; index >= 0; index -= 1) {
      if (!options[index]?.disabled) return index
    }
    return 0
  }

  function focusOption(): void {
    void nextTick(() => {
      config.root.value
        ?.querySelector<HTMLElement>(config.optionSelector(activeIndex.value))
        ?.focus()
    })
  }

  function openMenu(edge?: 'first' | 'last'): void {
    if (config.disabled() || config.options().length === 0) return
    open.value = true
    const selected = config.selectedIndex()
    activeIndex.value =
      edge === 'first'
        ? firstEnabledIndex()
        : edge === 'last'
          ? lastEnabledIndex()
          : selected >= 0
            ? selected
            : firstEnabledIndex()
    focusOption()
  }

  function closeMenu(returnFocus = false): void {
    open.value = false
    if (returnFocus) void nextTick(() => config.trigger.value?.focus())
  }

  function toggleMenu(): void {
    if (open.value) closeMenu(true)
    else openMenu()
  }

  function moveActive(offset: number): void {
    const options = config.options()
    const count = options.length
    if (count === 0) return
    let next = activeIndex.value
    for (let attempts = 0; attempts < count; attempts += 1) {
      next = (next + offset + count) % count
      if (!options[next]?.disabled) {
        activeIndex.value = next
        focusOption()
        return
      }
    }
  }

  function onTriggerKeydown(event: KeyboardEvent): void {
    switch (event.key) {
      case 'ArrowDown':
        event.preventDefault()
        if (open.value) moveActive(1)
        else openMenu('first')
        break
      case 'ArrowUp':
        event.preventDefault()
        if (open.value) moveActive(-1)
        else openMenu('last')
        break
      case 'Enter':
      case ' ':
        event.preventDefault()
        toggleMenu()
        break
      case 'Escape':
        // Only an open menu consumes Escape; otherwise a surrounding dialog closes.
        if (open.value) {
          event.preventDefault()
          event.stopPropagation()
          closeMenu(true)
        }
        break
    }
  }

  function onOptionKeydown(event: KeyboardEvent, index: number): void {
    activeIndex.value = index
    const option = config.options()[index]
    switch (event.key) {
      case 'ArrowDown':
        event.preventDefault()
        moveActive(1)
        break
      case 'ArrowUp':
        event.preventDefault()
        moveActive(-1)
        break
      case 'Home':
        event.preventDefault()
        activeIndex.value = firstEnabledIndex()
        focusOption()
        break
      case 'End':
        event.preventDefault()
        activeIndex.value = lastEnabledIndex()
        focusOption()
        break
      case 'Enter':
      case ' ':
        event.preventDefault()
        if (option && !option.disabled) config.select(option)
        break
      case 'Escape':
        event.preventDefault()
        event.stopPropagation()
        closeMenu(true)
        break
      case 'Tab':
        closeMenu()
        break
    }
  }

  function closeWhenOutside(event: Event): void {
    if (open.value && !config.root.value?.contains(event.target as Node)) closeMenu()
  }

  watch(config.disabled, disabled => {
    if (disabled) closeMenu()
  })

  watch(
    config.options,
    options => {
      if (options.length === 0) closeMenu()
      if (activeIndex.value >= options.length) activeIndex.value = firstEnabledIndex()
    },
    { deep: true }
  )

  if (getCurrentInstance() && typeof document !== 'undefined') {
    onMounted(() => {
      document.addEventListener('pointerdown', closeWhenOutside)
      document.addEventListener('focusin', closeWhenOutside)
    })
    onBeforeUnmount(() => {
      document.removeEventListener('pointerdown', closeWhenOutside)
      document.removeEventListener('focusin', closeWhenOutside)
    })
  }

  return {
    open,
    activeIndex,
    openMenu,
    closeMenu,
    toggleMenu,
    onTriggerKeydown,
    onOptionKeydown
  }
}
