import { readFile } from 'node:fs/promises'
import vm from 'node:vm'
import ts from 'typescript'
import { compileScript, parse } from 'vue/compiler-sfc'
import { createRenderer, h, toRaw } from 'vue'

// A renderer whose nodes are plain objects: props and listeners are recorded,
// `contains` follows the parent chain, so components run their real setup,
// template and lifecycle without a browser.
export function createTestRenderer() {
  const node = fields => ({
    children: [],
    props: {},
    parent: null,
    contains(other) {
      // A template ref holds a reactive proxy of the node, so compare raw objects.
      const self = toRaw(this)
      for (let current = toRaw(other); current; current = toRaw(current.parent)) {
        if (current === self) return true
      }
      return false
    },
    ...fields
  })
  return createRenderer({
    createElement: tag => node({ tag }),
    createText: text => node({ text }),
    createComment: text => node({ text }),
    insert(child, parent, anchor) {
      child.parent = parent
      const index = anchor ? parent.children.indexOf(anchor) : -1
      if (index >= 0) parent.children.splice(index, 0, child)
      else parent.children.push(child)
    },
    remove(child) {
      if (!child.parent) return
      child.parent.children = child.parent.children.filter(item => item !== child)
      child.parent = null
    },
    setText(target, text) { target.text = text },
    setElementText(target, text) { target.children = []; target.text = text },
    parentNode: target => target.parent,
    nextSibling(target) {
      const siblings = target.parent?.children || []
      return siblings[siblings.indexOf(target) + 1] || null
    },
    patchProp(target, key, _previous, next) { target.props[key] = next }
  })
}

export function findNodes(root, predicate, found = []) {
  if (predicate(root)) found.push(root)
  for (const child of root.children || []) findNodes(child, predicate, found)
  return found
}

export function hasClass(target, name) {
  return String(target.props?.class || '').split(/\s+/).includes(name)
}

/** Runs a composable inside a mounted component so lifecycle hooks register. */
export function withSetup(composable, { plugins = [] } = {}) {
  let result
  const app = createTestRenderer().createApp({
    setup() {
      result = composable()
      return () => null
    }
  })
  for (const plugin of plugins) app.use(plugin)
  app.mount({ children: [], props: {} })
  return { result, app, unmount: () => app.unmount() }
}

// Child .vue components render only their default slot, so a wrapper such as
// a transition stays transparent; icon components render nothing.
const passThrough = { setup: (_props, { slots }) => () => slots.default?.() }

/**
 * Compiles a production SFC (script setup and template) into a component.
 * `globals` supplies browser objects the setup reads, such as `document`.
 */
export async function loadComponent(url, { globals = {} } = {}) {
  const { descriptor } = parse(await readFile(url, 'utf8'))
  const script = compileScript(descriptor, {
    id: url.pathname,
    inlineTemplate: true,
    genDefaultAs: '__component'
  }).content
  const ast = ts.createSourceFile('component.ts', script, ts.ScriptTarget.Latest, true)
  const bindings = { ...globals }
  for (const node of ast.statements.filter(ts.isImportDeclaration)) {
    const clause = node.importClause
    if (!clause || clause.isTypeOnly) continue
    const specifier = node.moduleSpecifier.text
    let module
    if (specifier.endsWith('.vue')) module = { default: passThrough }
    else if (specifier === '@lucide/vue') module = new Proxy({}, { get: () => ({ render: () => null }) })
    else if (specifier.startsWith('.')) module = await import(new URL(`${specifier}.ts`, url).href)
    else module = await import(specifier)
    if (clause.name) bindings[clause.name.text] = module.default
    for (const item of clause.namedBindings?.elements || []) {
      if (!item.isTypeOnly) bindings[item.name.text] = module[item.propertyName?.text || item.name.text]
    }
  }
  const body = ast.statements
    .filter(node => !ts.isImportDeclaration(node))
    .map(node => node.getText(ast))
    .join('\n')
  return vm.runInNewContext(
    `${ts.transpileModule(body, {
      compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext }
    }).outputText}\n__component`,
    bindings
  )
}

export function mountComponent(component, { slots = {}, props = {}, plugins = [] } = {}) {
  const container = { children: [], props: {}, parent: null }
  const app = createTestRenderer().createApp({ render: () => h(component, props, slots) })
  for (const plugin of plugins) app.use(plugin)
  app.mount(container)
  return { app, container, unmount: () => app.unmount() }
}
