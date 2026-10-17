import { readFile } from 'node:fs/promises'
import vm from 'node:vm'
import ts from 'typescript'
import { compileScript, parse } from 'vue/compiler-sfc'
import { createRenderer, h, nextTick, reactive } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { gateway } from '../../src/api/client.ts'
import { createFixtureGateway } from '../../src/api/fixture.ts'
import { i18n } from '../../src/i18n/index.ts'
import { messageThreadRoute } from '../../src/router/messageRoute.ts'
import { resetWorkspaceState } from '../../src/state/workspace.ts'

// Compile the complete production setup, including every watcher and lifecycle
// hook. Only visual child components and the browser surface are substituted;
// routing, composer state, workspace reconciliation and API errors are real.
const viewURL = new URL('../../src/views/MessagesView.vue', import.meta.url)
const { descriptor } = parse(await readFile(viewURL, 'utf8'))
const script = compileScript(descriptor, { id: 'messages-test', genDefaultAs: 'Component' }).content
const ast = ts.createSourceFile('messages.ts', script, ts.ScriptTarget.Latest, true)
const bindings = {
  window: { addEventListener() {}, removeEventListener() {} },
  document: { visibilityState: 'visible', hasFocus: () => true, addEventListener() {}, removeEventListener() {} }
}
for (const node of ast.statements.filter(ts.isImportDeclaration)) {
  const clause = node.importClause
  if (!clause || clause.isTypeOnly) continue
  const specifier = node.moduleSpecifier.text
  const visual = specifier.endsWith('.vue') || specifier === '@lucide/vue'
  const module = visual ? {} : await import(specifier.startsWith('.')
    ? new URL(specifier + '.ts', viewURL).href : specifier)
  if (clause.name) bindings[clause.name.text] = visual ? {} : module.default
  for (const item of clause.namedBindings?.elements || []) {
    if (!item.isTypeOnly) bindings[item.name.text] = visual ? {} : module[item.propertyName?.text || item.name.text]
  }
}
const body = ast.statements.filter(node => !ts.isImportDeclaration(node)).map(node => node.getText(ast)).join('\n')
const Component = vm.runInNewContext(ts.transpileModule(body, {
  compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext }
}).outputText + '\nComponent', bindings)
Component.render = () => null

const renderer = createRenderer({
  createElement: tag => ({ tag, children: [] }),
  createText: text => ({ text }), createComment: text => ({ text }),
  insert(child, parent) { child.parent = parent; parent.children.push(child) },
  remove(child) { child.parent.children = child.parent.children.filter(item => item !== child) },
  setText(node, text) { node.text = text }, setElementText(node, text) { node.text = text },
  parentNode: node => node.parent, nextSibling: () => null, patchProp() {}
})

export async function flushView() {
  // Let the current chain of API/router microtasks drain, then render it. An
  // unresolved request stays pending; this does not advance wall-clock timers.
  await new Promise(resolve => setImmediate(resolve))
  await nextTick()
}

export async function mountMessagesView(t, initialProps = {}) {
  resetWorkspaceState()
  const original = { ...gateway }
  const fixture = createFixtureGateway()
  const bootstrap = await fixture.getBootstrap()
  const lineID = bootstrap.lines.find(line => line.capabilities?.message)?.id
  const meta = { limit: 100, has_more: false, next_cursor: '' }
  const threads = ['A', 'B'].map((key, i) => ({
    key, line_id: lineID, peer: `+1202555010${i + 1}`,
    last_timestamp: '2026-10-17T00:00:00Z', last_content: 'older',
    last_message_id: String(i + 1), unread_count: 0, marked_unread: false, favorite: false
  }))
  const messages = new Map(threads.map(thread => [thread.peer, [{
    id: thread.last_message_id, line_id: lineID, peer: thread.peer,
    direction: 'outgoing', content: 'older', timestamp: thread.last_timestamp
  }]]))
  const sends = []
  const requests = []
  const network = { failThreads: false, loadMessages: null }
  gateway.getBootstrap = async () => bootstrap
  gateway.listContacts = async () => ({ items: [], meta })
  gateway.listThreads = async () => {
    if (network.failThreads) throw new Error('synthetic thread refresh failure')
    return { items: threads.map(thread => ({ ...thread })), meta }
  }
  gateway.listMessages = async query => {
    requests.push(query)
    if (network.loadMessages) return network.loadMessages(query)
    return { items: [...(messages.get(query.peer) || [])], meta }
  }
  gateway.markThreadRead = async () => {}
  gateway.sendMessage = input => new Promise((resolve, reject) => {
    sends.push({ input, reject, complete() {
      const message = {
        id: String(100 + sends.length), line_id: input.line_id, peer: input.to,
        content: input.content, direction: 'outgoing', timestamp: '2026-10-18T00:00:00Z'
      }
      messages.set(input.to, [...(messages.get(input.to) || []), message])
      const thread = threads.find(item => item.peer === input.to && item.line_id === input.line_id)
      if (thread) Object.assign(thread, { last_content: message.content, last_timestamp: message.timestamp })
      resolve(message)
    } })
  })
  const router = createRouter({ history: createMemoryHistory(), routes: [
    { name: 'messages', path: '/messages/:threadRef?', component: {} }
  ] })
  await router.push(messageThreadRoute('A'))
  const events = []
  const props = reactive(initialProps)
  const app = renderer.createApp({ render: () => h(Component, {
    ...props, onSent: key => events.push(['sent', key]), onClose: () => events.push(['close'])
  }) })
  app.use(router)
  app.use(i18n)
  app.mount({ children: [] })
  let mounted = true
  const unmount = () => { if (mounted) { app.unmount(); mounted = false } }
  t.after(() => { unmount(); Object.assign(gateway, original); resetWorkspaceState() })
  const state = app._instance.subTree.component.setupState
  const viewport = { scrollTop: 0, scrollHeight: 1000, clientHeight: 200 }
  state.messagesViewport = viewport
  await flushView()
  return {
    state, props, router, events, sends, requests, network, threads, messages, meta, viewport, unmount,
    async open(key) { await router.push(messageThreadRoute(key)); await flushView() },
    readOlder() { viewport.scrollTop = 100; state.onMessagesScroll() }
  }
}
