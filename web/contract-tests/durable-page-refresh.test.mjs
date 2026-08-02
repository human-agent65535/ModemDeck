import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { effectScope } from 'vue'

import { gateway } from '../src/api/client.ts'
import { useListArrivals } from '../src/composables/useListArrivals.ts'
import {
  contactsResource,
  loadContacts,
  refreshContacts,
  resetWorkspaceState
} from '../src/state/workspace.ts'

const contactPage = items => ({
  items,
  meta: { limit: 50, next_cursor: '', has_more: false }
})

test('background collection refresh preserves ready state and arrival baselines', async () => {
  const originalListContacts = gateway.listContacts
  const initial = { id: 'contact-1', display_name: 'One', phones: [] }
  const incoming = { id: 'contact-2', display_name: 'Two', phones: [] }
  let releaseRefresh

  try {
    resetWorkspaceState()
    gateway.listContacts = async () => contactPage([initial])
    await loadContacts()

    const scope = effectScope()
    const arrivals = scope.run(() =>
      useListArrivals(
        () => ({
          items: contactsResource.data,
          ready: contactsResource.status === 'ready'
        }),
        contact => contact.id
      )
    )

    gateway.listContacts = () => new Promise(resolve => {
      releaseRefresh = resolve
    })
    const refresh = refreshContacts()

    assert.equal(contactsResource.status, 'ready')
    assert.deepEqual(contactsResource.data.map(contact => contact.id), ['contact-1'])

    releaseRefresh(contactPage([incoming, initial]))
    await refresh

    assert.equal(contactsResource.status, 'ready')
    assert.equal(arrivals.isArriving('contact-2'), true)
    scope.stop()
  } finally {
    gateway.listContacts = originalListContacts
    resetWorkspaceState()
  }
})

test('each durable page declares only its persisted list dependencies', async () => {
  const sources = Object.fromEntries(
    await Promise.all(
      ['Dashboard', 'Contacts', 'Messages', 'Calls', 'Recordings'].map(
        async name => [
          name,
          await readFile(
            new URL(`../src/views/${name}View.vue`, import.meta.url),
            'utf8'
          )
        ]
      )
    )
  )

  for (const source of Object.values(sources)) {
    assert.match(source, /useDurablePageRefresh/)
  }
  assert.match(
    sources.Dashboard,
    /useDurablePageRefresh\([\s\S]*?refreshCalls\(\)[\s\S]*?refreshThreads\(\)[\s\S]*?refreshContacts\(\)[\s\S]*?refreshRecordingWorkspace\(false\)/
  )
  assert.match(sources.Contacts, /useDurablePageRefresh\(\(\) => refreshContacts\(\)/)
  assert.match(
    sources.Messages,
    /useDurablePageRefresh\([\s\S]*?refreshContacts\(\)[\s\S]*?refreshMessageWorkspace\(selectedKey\.value\)/
  )
  assert.match(
    sources.Calls,
    /useDurablePageRefresh\([\s\S]*?refreshCalls\(\)[\s\S]*?refreshContacts\(\)[\s\S]*?refreshRecordingWorkspace\(false\)/
  )
  assert.match(
    sources.Recordings,
    /useDurablePageRefresh\([\s\S]*?refreshRecordingEntries\(search\.value\)[\s\S]*?refreshContacts\(\)/
  )
})
