import { computed, ref } from 'vue'

export function useListSelection<T>(keyFor: (item: T) => string) {
  const active = ref(false)
  const keys = ref(new Set<string>())
  const count = computed(() => keys.value.size)

  function enter(): void {
    active.value = true
    keys.value = new Set()
  }

  function exit(): void {
    active.value = false
    keys.value = new Set()
  }

  function toggleMode(): void {
    if (active.value) exit()
    else enter()
  }

  function has(item: T): boolean {
    return keys.value.has(keyFor(item))
  }

  function toggle(item: T): void {
    if (!active.value) active.value = true
    const next = new Set(keys.value)
    const key = keyFor(item)
    if (next.has(key)) next.delete(key)
    else next.add(key)
    keys.value = next
  }

  function selectAll(items: T[]): void {
    const visibleKeys = items.map(keyFor)
    const allSelected =
      visibleKeys.length > 0 && visibleKeys.every(key => keys.value.has(key))
    keys.value = allSelected ? new Set() : new Set(visibleKeys)
  }

  function selected(items: T[]): T[] {
    return items.filter(item => keys.value.has(keyFor(item)))
  }

  function clear(): void {
    keys.value = new Set()
  }

  function reconcile(items: T[]): void {
    const available = new Set(items.map(keyFor))
    const next = new Set(Array.from(keys.value).filter(key => available.has(key)))
    if (next.size !== keys.value.size) keys.value = next
  }

  return {
    active,
    keys,
    count,
    enter,
    exit,
    toggleMode,
    has,
    toggle,
    selectAll,
    selected,
    clear,
    reconcile
  }
}
