import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import ts from 'typescript'

// Render the dashboard's real hooks and event handlers with deferred API
// responses. No browser, live admin credentials or additional packages needed.
let tree, index = 0, scheduled = false, mounted = true
const hooks = [], effects = [], pending = []
const intervals = new Map()
let timerID = 0
globalThis.window = Object.assign(new EventTarget(), {
  location: { hash: '' },
  setInterval: callback => { intervals.set(++timerID, callback); return timerID },
  clearInterval: id => intervals.delete(id),
  setTimeout,
  clearTimeout,
})
globalThis.document = Object.assign(new EventTarget(), { querySelector: () => null })
const schedule = () => {
  if (!mounted || scheduled) return
  scheduled = true
  queueMicrotask(() => { scheduled = false; if (mounted) render() })
}
const react = {
  useState(initial) {
    const id = index++
    hooks[id] ??= { value: typeof initial === 'function' ? initial() : initial }
    return [hooks[id].value, value => {
      const next = typeof value === 'function' ? value(hooks[id].value) : value
      if (!Object.is(next, hooks[id].value)) { hooks[id].value = next; schedule() }
    }]
  },
  useRef(initial) { const id = index++; return hooks[id] ??= { current: initial } },
  useCallback(callback, deps) {
    const id = index++
    if (!hooks[id] || deps.some((v, i) => !Object.is(v, hooks[id].deps[i]))) hooks[id] = { deps, callback }
    return hooks[id].callback
  },
  useEffect(callback, deps) {
    const id = index++
    if (!hooks[id] || !deps || deps.some((v, i) => !Object.is(v, hooks[id].deps[i]))) {
      effects.push(() => { hooks[id]?.cleanup?.(); hooks[id] = { deps, cleanup: callback() } })
    }
  },
}
const fetchWithTimeout = async (path, init = {}) => {
  if (path === '/api/v1/auth/me') return new Response('{"username":"test"}')
  return new Promise((resolve, reject) => {
    const request = { path, resolve, reject, signal: init.signal }
    pending.push(request)
    init.signal?.addEventListener('abort', () => reject(init.signal.reason), { once: true })
  })
}
const source = await readFile(new URL('../src/components/AdminDashboard.tsx', import.meta.url), 'utf8')
const compiled = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.ReactJSX } }).outputText
const exports = {}
new Function('require', 'exports', compiled)(name => {
  if (name === 'react') return react
  if (name === 'react/jsx-runtime') return { jsx: (type, props) => ({ type, props }), jsxs: (type, props) => ({ type, props }) }
  if (name === '../request') return { fetchWithTimeout }
  if (name === '../appearance') return { useAppearance: () => ({ theme: 'blue-tech' }) }
  return {}
}, exports)
const render = () => { index = 0; tree = exports.AdminDashboard(); while (effects.length) effects.shift()() }
const settle = async () => { for (let i = 0; i < 4; i++) await new Promise(resolve => setImmediate(resolve)) }
const nodes = node => {
  if (Array.isArray(node)) return node.flatMap(nodes)
  if (!node || typeof node !== 'object') return [node]
  return [node, ...nodes(node.props?.children)]
}
const refreshButton = () => nodes(tree).find(node => node?.props?.className?.includes('refresh-console'))
const text = () => nodes(tree).filter(node => typeof node === 'string').join(' ')
render()
await settle()
assert.equal(pending.length, 2)
assert.match(text(), /取消刷新/)

// One timeout must cancel its sibling and let a new refresh start.
pending[0].reject(new Error('请求超时，请刷新状态后重试。'))
await settle()
assert.ok(pending[1].signal.aborted, 'sibling request survived refresh failure')
assert.match(text(), /请求超时/)
assert.match(text(), /刷新状态/)
refreshButton().props.onClick()
await settle()
assert.equal(pending.length, 4, 'timeout kept the refresh lock held')
pending[2].resolve(new Response('{"data":[]}'))
pending[3].resolve(new Response('{"data":{"channel_capacity":3}}'))
await settle()
assert.match(text(), /渲染服务在线/)
assert.doesNotMatch(text(), /请求超时/)

// Manual cancellation and automatic refresh must both remain usable.
refreshButton().props.onClick()
await settle()
refreshButton().props.onClick()
await settle()
assert.ok(pending[4].signal.aborted && pending[5].signal.aborted)
assert.match(text(), /刷新已取消/)
for (const callback of intervals.values()) callback()
await settle()
assert.equal(pending.length, 8, 'automatic refresh did not recover after cancellation')
mounted = false
for (const hook of hooks) hook?.cleanup?.()
await settle()
assert.ok(pending[6].signal.aborted && pending[7].signal.aborted, 'unmount left refresh requests running')
console.log('Dashboard refresh timeout recovery, sibling cancellation, retry, polling and unmount passed.')
