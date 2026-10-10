import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import ts from 'typescript'

// Exercise real theme state transitions with deferred server responses.
let hooks = [], effects = [], pending = [], index = 0, scheduled = false, mounted = true, state
let authenticated = true
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
    if (!hooks[id] || deps.some((v, i) => !Object.is(v, hooks[id].deps[i]))) {
      effects.push(() => { hooks[id]?.cleanup?.(); hooks[id] = { deps, cleanup: callback() } })
    }
  },
}
const compile = source => ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS } }).outputText
const themes = {}
new Function('exports', compile(await readFile(new URL('../src/themes.ts', import.meta.url), 'utf8')))(themes)
const exports = {}
new Function('require', 'exports', compile(await readFile(new URL('../src/appearance.ts', import.meta.url), 'utf8')))(name => name === 'react' ? react : themes, exports)
const request = (path, init) => new Promise((resolve, reject) => {
  assert.equal(path, '/api/v1/admin/appearance')
  pending.push({ init, resolve: theme => resolve(new Response(JSON.stringify({ data: { theme } }))), reject })
  init.signal.addEventListener('abort', () => reject(init.signal.reason), { once: true })
})
const render = () => { index = 0; state = exports.useAppearance(authenticated, request); while (effects.length) effects.shift()() }
const settle = async () => { for (let i = 0; i < 4; i++) await new Promise(resolve => setImmediate(resolve)) }
const unmount = async () => { mounted = false; for (const hook of hooks) hook?.cleanup?.(); await settle() }
const reopen = async () => { await unmount(); hooks = []; effects = []; pending = []; mounted = true; render(); await settle() }

render()
await settle()
assert.equal(state.loading, true)
await state.save()
assert.equal(pending.length, 1, 'saved before loading the database setting')
pending[0].resolve('emerald')
await settle()
assert.equal(state.theme, 'emerald')
assert.equal(state.savedTheme, 'emerald')
state.select('aurora')
await settle()
assert.equal(state.theme, 'aurora')
assert.equal(state.savedTheme, 'emerald')
assert.equal(pending.length, 1, 'preview automatically persisted')
const saving = state.save()
await state.save()
await settle()
assert.equal(state.saving, true)
assert.equal(pending.length, 2, 'double click submitted twice')
assert.deepEqual(JSON.parse(pending[1].init.body), { theme: 'aurora' })
assert.equal(state.result, null, 'success shown before server confirmation')
pending[1].resolve('aurora')
await saving
await settle()
assert.equal(state.savedTheme, 'aurora')
assert.equal(state.result.success, true)
state.dismissResult()
state.select('rose')
await settle()
const failure = state.save()
pending[2].reject(new Error('数据库写入失败'))
await failure
await settle()
assert.equal(state.result.success, false)
assert.match(state.result.message, /数据库写入失败/)
assert.equal(state.savedTheme, 'aurora')
assert.equal(state.theme, 'rose', 'failed save discarded the selected preview')
assert.equal(state.saving, false)
const retry = state.save()
pending[3].resolve('rose')
await retry
await settle()
assert.equal(state.result.success, true)

await reopen()
pending[0].resolve('rose')
await settle()
assert.equal(state.theme, 'rose', 'reopen did not restore server theme')
state.select('gold')
await settle()
await reopen()
pending[0].resolve('rose')
await settle()
assert.equal(state.theme, 'rose', 'unsaved preview survived reopen')

await reopen()
pending[0].reject(new Error('配色读取失败'))
await settle()
assert.match(state.error, /配色读取失败/)
assert.equal(state.savedTheme, null)
await state.save()
assert.equal(pending.length, 1, 'failed read allowed overwriting unknown setting')
state.retry()
await settle()
pending[1].resolve('glacier')
await settle()
assert.equal(state.error, '')
assert.equal(state.theme, 'glacier')
const abort = state.save()
authenticated = false
render()
await abort
await settle()
assert.ok(pending[2].init.signal.aborted, 'logout left theme save running')
assert.equal(state.result, null, 'cancelled save showed a result after logout')
await unmount()
console.log('Theme restore, preview, explicit save, confirmation, failure/retry, reload and logout cancellation passed.')
