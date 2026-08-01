import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

async function source(path) {
  return readFile(new URL(path, import.meta.url), 'utf8')
}

test('communication pages use one master-detail and header system', async () => {
  const [contacts, messages, calls, recordings, dashboard] = await Promise.all([
    source('../src/views/ContactsView.vue'),
    source('../src/views/MessagesView.vue'),
    source('../src/views/CallsView.vue'),
    source('../src/views/RecordingsView.vue'),
    source('../src/views/DashboardView.vue')
  ])

  for (const view of [contacts, messages, calls, recordings]) {
    assert.match(view, /import WorkspaceMasterDetail from/)
    assert.match(view, /import WorkspaceListHeader from/)
    assert.match(view, /import CommunicationListToolbar from/)
    assert.match(view, /import WorkspaceDetailPane from/)
    assert.match(view, /import WorkspaceDetailHeader from/)
    assert.match(view, /import WorkspaceDetailActions from/)
    assert.match(view, /import FavoriteActionButton from/)
    assert.match(view, /<WorkspaceMasterDetail/)
    assert.match(view, /<WorkspaceListHeader/)
    assert.match(view, /<CommunicationListToolbar/)
    assert.match(view, /<WorkspaceDetailPane[\s\S]*:content-key=/)
    assert.match(view, /<WorkspaceDetailHeader/)
    assert.doesNotMatch(view, /<WorkspaceDetailHeader[^>]*density=/)
    assert.match(view, /<WorkspaceDetailActions/)
    assert.match(view, /<FavoriteActionButton/)
    assert.doesNotMatch(view, /<header class="detail-header"/)
    assert.doesNotMatch(view, /<header class="conversation-header"/)
  }

  assert.match(dashboard, /import WorkspaceMasterDetail from/)
  assert.match(dashboard, /import WorkspaceListHeader from/)
  assert.match(dashboard, /<WorkspaceMasterDetail/)
  assert.match(
    dashboard,
    /<WorkspaceListHeader[\s\S]*compact-mode="hidden"/
  )
  assert.doesNotMatch(
    dashboard,
    /\.dashboard-activity-action \{[\s\S]*width: 38px;/
  )
  assert.doesNotMatch(
    dashboard,
    /\.dashboard-activity-pane > \.pane-header/
  )
})

test('workspace primitives own action sizing, priority, and transition timing', async () => {
  const [
    master,
    listHeader,
    listToolbar,
    detailHeader,
    detailActions,
    detailPane,
    favorite,
    styles
  ] =
    await Promise.all([
      source('../src/components/workspace/WorkspaceMasterDetail.vue'),
      source('../src/components/workspace/WorkspaceListHeader.vue'),
      source('../src/components/workspace/CommunicationListToolbar.vue'),
      source('../src/components/workspace/WorkspaceDetailHeader.vue'),
      source('../src/components/workspace/WorkspaceDetailActions.vue'),
      source('../src/components/workspace/WorkspaceDetailPane.vue'),
      source('../src/components/workspace/FavoriteActionButton.vue'),
      source('../src/style.css')
    ])

  assert.match(master, /'has-selection': hasSelection/)
  assert.match(master, /'is-batch-selecting': batchSelecting/)
  assert.match(master, /'is-embedded': embedded/)
  assert.match(master, /<aside v-if="!embedded" class="list-pane"/)
  assert.match(listHeader, /class="pane-header workspace-list-header"/)
  assert.match(listHeader, /<slot name="actions" \/>/)
  assert.match(listHeader, /compactMode\?: 'default' \| 'hidden' \| 'floating-action'/)
  assert.match(
    listHeader,
    /\.workspace-list-header \{[\s\S]*gap: var\(--detail-action-gap\);/
  )
  assert.match(
    listHeader,
    /\.workspace-list-header__title \{[\s\S]*margin-right: auto;/
  )
  assert.match(
    listHeader,
    /@media \(max-width: 860px\)[\s\S]*\.workspace-list-header\.workspace-list-header--compact-hidden \{[\s\S]*display: none;/
  )
  assert.doesNotMatch(detailHeader, /density\?:/)
  assert.match(
    detailHeader,
    /\.workspace-detail-header \{[\s\S]*min-height: 60px;[\s\S]*padding: 7px 20px;/
  )
  assert.match(detailHeader, /class="workspace-detail-header__actions"/)
  assert.match(detailHeader, /gap: var\(--detail-action-gap\);/)
  assert.doesNotMatch(detailHeader, /--detail-action-size:/)
  assert.match(detailHeader, /container-type: inline-size;/)
  assert.match(listToolbar, /class="pane-search communication-list-toolbar"/)
  assert.match(listToolbar, /hasLineFilter\?: boolean/)
  assert.match(detailActions, /class="workspace-detail-actions__primary"/)
  assert.match(detailActions, /class="workspace-detail-actions__secondary"/)
  assert.match(detailActions, /workspace-detail-actions__more/)
  assert.match(detailActions, /\.workspace-detail-command \{/)
  assert.match(detailActions, /@container \(max-width: 760px\)/)
  assert.match(styles, /--detail-action-gap: var\(--space-2\);/)
  assert.match(styles, /--detail-action-size: 40px;/)
  assert.match(
    styles,
    /@media \(max-width: 860px\)[\s\S]*:root \{[\s\S]*--detail-action-size: var\(--touch-target\);/
  )
  assert.match(
    styles,
    /\.icon-button \{[\s\S]*width: var\(--detail-action-size\);[\s\S]*height: var\(--detail-action-size\);/
  )

  assert.doesNotMatch(detailPane, /<Transition/)
  assert.match(detailPane, /:key="`detail:\$\{String\(contentKey\)\}`"/)
  assert.doesNotMatch(detailPane, /workspace-detail-content-in/)
  assert.doesNotMatch(detailPane, /opacity:\s*0/)
  assert.match(
    styles,
    /\.list-pane,[\s\S]*\.detail-pane \{[\s\S]*transform var\(--motion-slow\)/
  )
  assert.match(styles, /@media \(prefers-reduced-motion: reduce\)/)
  assert.match(favorite, /:aria-pressed="active"/)
  assert.match(favorite, /<Star :size="18" :fill="active \? 'currentColor' : 'none'"/)
  assert.match(favorite, /workspace-favorite-action__label/)
})
