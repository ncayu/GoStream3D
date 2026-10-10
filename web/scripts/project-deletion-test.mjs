import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import ts from 'typescript'
const source = await readFile(new URL('../src/project-deletion.ts', import.meta.url), 'utf8')
const compiled = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS } }).outputText
const exports = {}
new Function('exports', compiled)(exports)
const { deleteProjects } = exports
const calls = [], progress = []
const controller = new AbortController()
const result = await deleteProjects(['a', 'b', 'a', 'c'], async token => {
  calls.push(token)
  if (token === 'b') throw new Error('数据库保存失败')
}, controller.signal, value => progress.push(value))
assert.deepEqual(calls, ['a', 'b', 'c'])
assert.deepEqual(result.deleted, ['a', 'c'])
assert.deepEqual(result.failed, [{ token: 'b', message: '数据库保存失败' }])
assert.deepEqual(progress.at(-1), { completed: 3, total: 3 })
const retry = await deleteProjects(result.failed.map(item => item.token), async token => calls.push(token), controller.signal, () => {})
assert.deepEqual(retry.deleted, ['b'])
assert.deepEqual(calls, ['a', 'b', 'c', 'b'], 'retry resubmitted successes')
const missing = await deleteProjects(['gone'], async () => { throw Object.assign(new Error('not found'), { status: 404 }) }, controller.signal, () => {})
assert.deepEqual(missing.deleted, ['gone'])
assert.equal(missing.failed.length, 0)
const expiredCalls = []
const expired = await deleteProjects(['a', 'b', 'c'], async token => { expiredCalls.push(token); throw Object.assign(new Error('请重新登录'), { status: 401 }) }, controller.signal, () => {})
assert.deepEqual(expiredCalls, ['a'])
assert.deepEqual(expired.skipped, ['b', 'c'])
const aborted = new AbortController(), abortCalls = []
const cancelled = await deleteProjects(['a', 'b', 'c'], async token => { abortCalls.push(token); aborted.abort(); throw new Error('cancelled') }, aborted.signal, () => {})
assert.deepEqual(abortCalls, ['a'])
assert.deepEqual(cancelled.skipped, ['b', 'c'])
assert.match(cancelled.failed[0].message, /结果未确认/)
console.log('Bulk deletion deduplication, partial failure, retry, already removed, auth expiry and cancellation passed.')
