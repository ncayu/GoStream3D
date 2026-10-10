import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { createServer } from 'node:http'
import ts from 'typescript'

const source = await readFile(new URL('../src/request.ts', import.meta.url), 'utf8')
const compiled = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 } }).outputText
const { fetchWithTimeout } = await import(`data:text/javascript;base64,${Buffer.from(compiled).toString('base64')}`)
const server = createServer((req, res) => {
  if (req.url === '/headers-stall') return
  if (req.url === '/body-stall') { res.writeHead(200, { 'Content-Type': 'application/json' }); res.write('{'); return }
  if (req.url === '/empty') { res.writeHead(204); res.end(); return }
  if (req.url === '/error') { res.writeHead(503, { 'Content-Type': 'application/json' }); res.end('{"msg":"busy"}'); return }
  res.writeHead(200, { 'Content-Type': 'application/json' })
  res.end('{"data":[1,2]}')
})
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve))
const base = `http://127.0.0.1:${server.address().port}`
try {
  for (const path of ['/headers-stall', '/body-stall']) {
    const start = performance.now()
    let guard
    try {
      await assert.rejects(Promise.race([
        fetchWithTimeout(base + path, {}, 100),
        new Promise((_, reject) => { guard = setTimeout(() => reject(new Error('Request stayed stuck past its deadline')), 1500) }),
      ]), /请求超时/)
    } finally { clearTimeout(guard) }
    assert.ok(performance.now() - start < 1500, 'stalled request did not finish on time')
  }
  const controller = new AbortController()
  const pending = fetchWithTimeout(base + '/body-stall', { signal: controller.signal }, 5000)
  controller.abort()
  await assert.rejects(pending, error => error.name === 'AbortError')
  await assert.rejects(fetchWithTimeout(base, { signal: controller.signal }, 100), error => error.name === 'AbortError')
  const retry = await fetchWithTimeout(base, {}, 1000)
  assert.deepEqual(await retry.json(), { data: [1, 2] }, 'retry after timeout failed')
  const empty = await fetchWithTimeout(base + '/empty', {}, 1000)
  assert.equal(empty.status, 204)
  assert.equal(await empty.text(), '')
  const error = await fetchWithTimeout(base + '/error', {}, 1000)
  assert.equal(error.status, 503)
  assert.deepEqual(await error.json(), { msg: 'busy' })
  console.log('Request header/body timeouts, cancellation, retry and response compatibility passed.')
} finally {
  server.closeAllConnections()
  await new Promise(resolve => server.close(resolve))
}
